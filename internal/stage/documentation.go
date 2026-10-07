package stage

import (
	"context"
	"fmt"
	"io/fs"
	"math/rand/v2"
	"os"
	"path/filepath"
	"slop-generator/internal/form"
	"slop-generator/internal/inference"
	"strings"
)

type FileSource struct {
	Filename string
	Content  string
}

type Documentation struct {
	SourceFilename string
	Markdown       string
}

// FileReadStage picks one matching file from the task's private worktree.
type FileReadConfig struct{ Extension string }

func FileReadFields(id string, values map[string]string) ([]form.Field, error) {
	for key := range values {
		if key != "extension" {
			return nil, fmt.Errorf("stage %s: unknown setting %s", id, key)
		}
	}
	field := form.Field{ID: id + ".extension", Label: "File extension (for example .go)", Value: values["extension"], Kind: form.Text, Required: true}
	field.Check = func(value string) error {
		if !validExtension(value) {
			return fmt.Errorf("extension must start with a dot and contain only letters, digits, dot, underscore, plus or hyphen")
		}
		return nil
	}
	if value := values["extension"]; value != "" {
		if err := field.Validate(value); err != nil {
			return nil, err
		}
		return nil, nil
	}
	return []form.Field{field}, nil
}

func ConfigureFileRead(values map[string]string) FileReadConfig {
	return FileReadConfig{Extension: values["extension"]}
}

func validExtension(value string) bool {
	if len(value) < 2 || value[0] != '.' {
		return false
	}
	hasLetterOrDigit := false
	for _, r := range value[1:] {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("._+-", r)) {
			return false
		}
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
			hasLetterOrDigit = true
		}
	}
	return hasLetterOrDigit
}

type FileReadStage struct {
	Worktree Worktree
	Config   FileReadConfig
}

func (s *FileReadStage) Run(ctx context.Context, _ *RunContext, _ Input[Artifacts, Unit]) (Output[FileSource, Unit], error) {
	if s.Worktree == nil {
		return Output[FileSource, Unit]{}, fmt.Errorf("worktree is unavailable")
	}
	root := s.Worktree.Root()
	var files []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry.IsDir() {
			if path != root && (entry.Name() == ".git" || entry.Type()&os.ModeSymlink != 0) {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.Type()&os.ModeSymlink == 0 && entry.Type().IsRegular() && strings.EqualFold(filepath.Ext(entry.Name()), s.Config.Extension) {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		return Output[FileSource, Unit]{}, fmt.Errorf("scan worktree for matching files: %w", err)
	}
	if len(files) == 0 {
		return Output[FileSource, Unit]{}, fmt.Errorf("worktree contains no files with extension %s", s.Config.Extension)
	}
	path := files[rand.IntN(len(files))]
	content, err := os.ReadFile(path)
	if err != nil {
		return Output[FileSource, Unit]{}, fmt.Errorf("read source file: %w", err)
	}
	name, err := filepath.Rel(root, path)
	if err != nil {
		return Output[FileSource, Unit]{}, err
	}
	return Output[FileSource, Unit]{Status: Success, Result: FileSource{Filename: filepath.ToSlash(name), Content: string(content)}}, nil
}

// InferenceStage asks the configured model to document the selected source file.
// Lint feedback is added to the existing conversation on a retry.
type InferenceStage struct {
	Generator   Generator
	RecordUsage func(*inference.Usage)
	Language    string
	messages    []inference.Message
}

var documentationLanguages = []string{"English", "Russian", "Spanish", "French", "German", "Italian", "Portuguese", "Chinese", "Japanese", "Korean", "Arabic"}

func DocumentationFields(id string, values map[string]string) ([]form.Field, error) {
	for key := range values {
		if key != "language" {
			return nil, fmt.Errorf("stage %s: unknown setting %s", id, key)
		}
	}
	field := form.Field{ID: id + ".language", Label: "Documentation language", Value: values["language"], Kind: form.Choice, Options: documentationLanguages, Required: true}
	if value := values["language"]; value != "" {
		if err := field.Validate(value); err != nil {
			return nil, err
		}
		return nil, nil
	}
	return []form.Field{field}, nil
}

func (s *InferenceStage) Run(ctx context.Context, _ *RunContext, in Input[FileSource, Diagnostic]) (Output[Documentation, Unit], error) {
	if in.Feedback == nil {
		s.messages = []inference.Message{
			{Role: "system", Content: "Generate clear, accurate technical documentation for the provided source file in " + s.Language + ". Return only Markdown. Use a concise title, describe the class and its public behavior, document important methods and parameters when present, and use fenced code blocks with a language when useful. Keep lines reasonably short."},
			{Role: "user", Content: "Source file: " + in.Result.Filename + "\n\n```\n" + in.Result.Content + "\n```"},
		}
	} else {
		s.messages = append(s.messages, inference.Message{Role: "user", Content: "The Markdown lint stage reported these issues. Fix them and return the complete corrected Markdown document only:\n" + in.Feedback.Message})
	}
	result, err := s.Generator.Generate(ctx, inference.Request{Messages: append([]inference.Message(nil), s.messages...)})
	if s.RecordUsage != nil {
		s.RecordUsage(result.Usage)
	}
	if err != nil {
		return Output[Documentation, Unit]{}, err
	}
	markdown := strings.TrimSpace(result.Content)
	if markdown == "" {
		return Output[Documentation, Unit]{}, fmt.Errorf("empty generated documentation")
	}
	s.messages = append(s.messages, inference.Message{Role: "assistant", Content: markdown})
	return Output[Documentation, Unit]{Status: Success, Result: Documentation{SourceFilename: in.Result.Filename, Markdown: markdown}}, nil
}

// MarkdownLintStage is intentionally a no-op for now. Keep the typed failure
// feedback contract so a real Markdown linter can be added without changing
// the pipeline connections; return Failure with Diagnostic feedback then.
type MarkdownLintStage struct{}

func (s *MarkdownLintStage) Run(_ context.Context, _ *RunContext, in Input[Documentation, Unit]) (Output[Documentation, Diagnostic], error) {
	return Output[Documentation, Diagnostic]{Status: Success, Result: in.Result}, nil
}

// FileWriteStage stores documentation in the task output directory. Its
// artifact path is executor metadata used by the pipeline's automatic commit.
type FileWriteStage struct{ Workspace Workspace }

func (s *FileWriteStage) Run(ctx context.Context, _ *RunContext, in Input[Documentation, Unit]) (Output[Artifacts, Unit], error) {
	if err := ctx.Err(); err != nil {
		return Output[Artifacts, Unit]{}, err
	}
	if s.Workspace == nil {
		return Output[Artifacts, Unit]{}, fmt.Errorf("output workspace is unavailable")
	}
	source := filepath.FromSlash(in.Result.SourceFilename)
	extension := filepath.Ext(source)
	if extension == "" || !filepath.IsLocal(source) {
		return Output[Artifacts, Unit]{}, fmt.Errorf("invalid source filename %q", in.Result.SourceFilename)
	}
	relative := filepath.Join("docs", strings.TrimSuffix(source, extension)+".md")
	path, err := s.Workspace.Path(relative)
	if err != nil {
		return Output[Artifacts, Unit]{}, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return Output[Artifacts, Unit]{}, err
	}
	if err := os.WriteFile(path, []byte(in.Result.Markdown+"\n"), 0600); err != nil {
		return Output[Artifacts, Unit]{}, err
	}
	return Output[Artifacts, Unit]{Status: Success, Result: Artifacts{{Path: filepath.ToSlash(relative)}}}, nil
}

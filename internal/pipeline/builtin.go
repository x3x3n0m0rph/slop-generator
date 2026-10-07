package pipeline

import (
	"fmt"
	"path/filepath"
	"slop-generator/internal/config"
	"slop-generator/internal/form"
	"slop-generator/internal/stage"
)

// PythonFunctions and ShortTexts are separate, code-defined pipeline types.
// Their Build methods construct fresh, named stage instances for each task.
type PythonFunctions struct{}
type ShortTexts struct{}
type DocumentationGeneration struct{}

func Builtins() map[string]Definition {
	return map[string]Definition{
		"python-functions": PythonFunctions{},
		"short-texts":      ShortTexts{},
		"documentation":    DocumentationGeneration{},
	}
}

type stageProfile struct {
	id     string
	python bool
}

func (PythonFunctions) Fields(p config.Pipeline) ([]form.Field, error) {
	return fieldsFor(p, []stageProfile{
		{id: "insertion-sort", python: true},
		{id: "read-lines", python: true},
		{id: "quadratic", python: true},
	})
}

func (ShortTexts) Fields(p config.Pipeline) ([]form.Field, error) {
	return fieldsFor(p, []stageProfile{
		{id: "sorting-guide"},
		{id: "file-reading"},
	})
}

func fieldsFor(p config.Pipeline, profiles []stageProfile) ([]form.Field, error) {
	allowed := make(map[string]bool, len(profiles)*2)
	filenames := map[string]bool{}
	var fields []form.Field
	for _, profile := range profiles {
		generateID := profile.id + ".generate"
		fileID := profile.id + ".write"
		if profile.python {
			fileID = profile.id + ".validate"
		}
		allowed[generateID], allowed[fileID] = true, true
		generationFields, err := stage.GenerationFields(generateID, p.Stages[generateID])
		if err != nil {
			return nil, err
		}
		for i := range generationFields {
			if profile.python && generationFields[i].ID == generateID+".filename" {
				baseCheck := generationFields[i].Check
				generationFields[i].Check = func(value string) error {
					if err := baseCheck(value); err != nil {
						return err
					}
					if filepath.Ext(value) != ".py" {
						return fmt.Errorf("filename must end in .py")
					}
					return nil
				}
			}
		}
		fields = append(fields, generationFields...)
		filename := p.Stages[generateID]["filename"]
		if filename != "" {
			if !config.SafeRelative(filename) {
				return nil, fmt.Errorf("stage %s: unsafe filename", generateID)
			}
			clean := filepath.Clean(filename)
			if filenames[clean] {
				return nil, fmt.Errorf("duplicate filename %s", filename)
			}
			filenames[clean] = true
			if profile.python && filepath.Ext(filename) != ".py" {
				return nil, fmt.Errorf("stage %s: filename must end in .py", generateID)
			}
		}
		if profile.python {
			if _, err := stage.ValidationFields(fileID, p.Stages[fileID]); err != nil {
				return nil, err
			}
		} else if len(p.Stages[fileID]) != 0 {
			return nil, fmt.Errorf("stage %s has no configuration", fileID)
		}
	}
	for id := range p.Stages {
		if !allowed[id] {
			return nil, fmt.Errorf("unknown stage %s", id)
		}
	}
	return fields, nil
}

func generation(p config.Pipeline, r Resources, id string, python bool) *stage.Generate {
	return &stage.Generate{Config: stage.ConfigureGeneration(p.Stages[id]), Generator: r.Generator, Python: python, RecordUsage: r.RecordUsage}
}

func outputStage(p config.Pipeline, r Resources, id, artifactID string, python bool) *stage.WriteFile {
	return &stage.WriteFile{
		Workspace: r.Workspace,
		Config:    stage.ConfigureValidation(p.Stages[id]),
		Check:     python,
		Cache:     filepath.Join(r.CacheDir, artifactID+".pyc"),
	}
}

func checkComplete(fields []form.Field, err error) error {
	if err != nil {
		return err
	}
	if len(fields) != 0 {
		return fmt.Errorf("stage configuration incomplete")
	}
	return nil
}

func withCommit(p config.Pipeline, r Resources, chain Chain[stage.Artifacts, stage.Artifacts, stage.Unit]) Chain[stage.Artifacts, stage.Artifacts, stage.Unit] {
	if p.Config.CommitMode == "stages" {
		commitGeneratedArtifacts := &stage.Commit{Committer: r.Committer, Message: "Save generated artifacts"}
		return Then(chain, "commit", commitGeneratedArtifacts)
	}
	return chain
}

func (p PythonFunctions) Build(c config.Pipeline, r Resources) (Chain[stage.Artifacts, stage.Artifacts, stage.Unit], error) {
	if err := checkComplete(p.Fields(c)); err != nil {
		return Chain[stage.Artifacts, stage.Artifacts, stage.Unit]{}, err
	}

	generateInsertionSort := generation(c, r, "insertion-sort.generate", true)
	validateInsertionSort := outputStage(c, r, "insertion-sort.validate", "insertion-sort", true)
	generateReadLines := generation(c, r, "read-lines.generate", true)
	validateReadLines := outputStage(c, r, "read-lines.validate", "read-lines", true)
	generateQuadratic := generation(c, r, "quadratic.generate", true)
	validateQuadratic := outputStage(c, r, "quadratic.validate", "quadratic", true)

	insertionSortGenerated := Start[stage.Artifacts, stage.Candidate, stage.Diagnostic]("insertion-sort.generate", generateInsertionSort)
	insertionSortValidated := Then(insertionSortGenerated, "insertion-sort.validate", validateInsertionSort)
	readLinesGenerated := Then(insertionSortValidated, "read-lines.generate", generateReadLines)
	readLinesValidated := Then(readLinesGenerated, "read-lines.validate", validateReadLines)
	quadraticGenerated := Then(readLinesValidated, "quadratic.generate", generateQuadratic)
	quadraticValidated := Then(quadraticGenerated, "quadratic.validate", validateQuadratic)
	return withCommit(c, r, quadraticValidated), nil
}

func (p ShortTexts) Build(c config.Pipeline, r Resources) (Chain[stage.Artifacts, stage.Artifacts, stage.Unit], error) {
	if err := checkComplete(p.Fields(c)); err != nil {
		return Chain[stage.Artifacts, stage.Artifacts, stage.Unit]{}, err
	}

	generateSortingGuide := generation(c, r, "sorting-guide.generate", false)
	writeSortingGuide := outputStage(c, r, "sorting-guide.write", "sorting-guide", false)
	generateFileReading := generation(c, r, "file-reading.generate", false)
	writeFileReading := outputStage(c, r, "file-reading.write", "file-reading", false)

	sortingGuideGenerated := Start[stage.Artifacts, stage.Candidate, stage.Diagnostic]("sorting-guide.generate", generateSortingGuide)
	sortingGuideWritten := Then(sortingGuideGenerated, "sorting-guide.write", writeSortingGuide)
	fileReadingGenerated := Then(sortingGuideWritten, "file-reading.generate", generateFileReading)
	fileReadingWritten := Then(fileReadingGenerated, "file-reading.write", writeFileReading)
	return withCommit(c, r, fileReadingWritten), nil
}

func (DocumentationGeneration) Fields(p config.Pipeline) ([]form.Field, error) {
	if p.Config.CommitMode != "stages" {
		return nil, fmt.Errorf("documentation pipeline requires commit_mode stages")
	}
	fields, err := stage.FileReadFields("file-read", p.Stages["file-read"])
	if err != nil {
		return nil, err
	}
	languageFields, err := stage.DocumentationFields("inference", p.Stages["inference"])
	if err != nil {
		return nil, err
	}
	fields = append(fields, languageFields...)
	for id, values := range p.Stages {
		if id != "file-read" && id != "inference" && id != "markdown-lint" {
			return nil, fmt.Errorf("unknown stage %s", id)
		}
		if id == "markdown-lint" && len(values) != 0 {
			return nil, fmt.Errorf("stage %s has no configuration", id)
		}
	}
	return fields, nil
}

func (p DocumentationGeneration) Build(c config.Pipeline, r Resources) (Chain[stage.Artifacts, stage.Artifacts, stage.Unit], error) {
	if err := checkComplete(p.Fields(c)); err != nil {
		return Chain[stage.Artifacts, stage.Artifacts, stage.Unit]{}, err
	}
	// Each task gets its own stage instances; their concrete types make the
	// source -> documentation -> lint -> write -> generated-message commit.
	readFile := &stage.FileReadStage{Worktree: r.Worktree, Config: stage.ConfigureFileRead(c.Stages["file-read"])}
	generateDocumentation := &stage.InferenceStage{Generator: r.Generator, RecordUsage: r.RecordUsage, Language: c.Stages["inference"]["language"]}
	markdownLint := &stage.MarkdownLintStage{}
	writeDocumentation := &stage.FileWriteStage{Workspace: r.Workspace}

	fileRead := Start[stage.Artifacts, stage.FileSource, stage.Unit]("file-read", readFile)
	documentationGenerated := Then(fileRead, "inference", generateDocumentation)
	documentationLinted := Then(documentationGenerated, "markdown-lint", markdownLint)
	documentationWritten := Then(documentationLinted, "file-write", writeDocumentation)
	commitDocumentation := &stage.DocumentationCommitStage{Generator: r.Generator, Committer: r.Committer, Context: r.CommitContext, RecordUsage: r.RecordUsage}
	return Then(documentationWritten, "commit", commitDocumentation), nil
}

func (PythonFunctions) StageCount(c config.Pipeline) int         { return stageCount(c, 6) }
func (ShortTexts) StageCount(c config.Pipeline) int              { return stageCount(c, 4) }
func (DocumentationGeneration) StageCount(c config.Pipeline) int { return stageCount(c, 4) }
func stageCount(c config.Pipeline, count int) int {
	if c.Config.CommitMode == "stages" {
		return count + 1
	}
	return count
}

func (e *Engine) Fields(p config.Pipeline) ([]form.Field, error) {
	if p.Retries() < 0 {
		return nil, fmt.Errorf("max_retries must not be negative")
	}
	if p.Config.CommitMode != "" && p.Config.CommitMode != "pipeline" && p.Config.CommitMode != "stages" {
		return nil, fmt.Errorf("invalid commit_mode")
	}
	d, ok := e.Definitions[p.Definition]
	if !ok {
		return nil, fmt.Errorf("unknown pipeline definition %q", p.Definition)
	}
	return d.Fields(p)
}

func (e *Engine) StageCount(p config.Pipeline) int {
	if d, ok := e.Definitions[p.Definition]; ok {
		return d.StageCount(p)
	}
	return 0
}

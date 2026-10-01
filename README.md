# slop-generator

Go TUI for running fixed LLM pipelines and committing their output to Git.

## Run

Requires Go 1.26+, Git, and Python 3 for the Python pipeline. Supports Windows and Linux terminals.

All settings and pipeline jobs live in a single YAML file. Copy `config.example.yaml` to `config.yaml`, configure the provider API key and optional SOCKS5 URL, an existing local repository, its remote and branch, and choose an available provider model. Paths are relative to this configuration file. Both the local and remote target branches must already exist. Configure Git authentication in advance; commands run without interactive credential prompts.

Startup and `-check-config` verify each repository path exists, is a directory and is the root of a Git checkout, worktree or bare repository; the local branch must have a commit. A remote is not required for startup or task creation. Errors include the repository profile and offending path or setting. These checks are local and do not contact the remote. A nonexistent path is not created automatically. The sample `../playground` path must be replaced with your repository. Checks run again when a task is created; the configured remote, its accessibility and branch ancestry are checked during task preflight before inference.

Errors before the TUI identify the startup stage (configuration, Git validation, history storage, task service, or TUI), the relevant file, and the configuration field when available. Repository path errors show the resolved path and a correction hint without repeating Windows system-call diagnostics.

A newly initialized repository may have a selected branch such as `master` with no commits yet. Validation reports this separately from a missing branch. Create an initial commit before running a task; the application does not create it automatically.

```powershell
Copy-Item config.example.yaml config.yaml
go run ./cmd/slop-generator -config config.yaml -check-config
go run ./cmd/slop-generator -config config.yaml
```

Set `api_key` and optional `socks5: socks5://user:password@host:port` directly in the configuration. `config.yaml` is ignored by Git because it may contain credentials. Keep it outside target repositories, and never put secrets in prompts. SOCKS5 applies to inference only. For existing installations, `key_env` and `key_file` remain optional alternatives to `api_key` (choose exactly one key source); `socks5_file` remains an alternative to `socks5`.

Proxy files also support three lines: `host:port`, `user: USERNAME`, `pass: PASSWORD`, matching the supplied `socks5.txt` format.

Defaults: two concurrent tasks, 4096 output tokens/request, 120-second API and Git timeouts, 30-second Python checks. `max_tokens`, `timeout_seconds`, `parallelism`, and the Python executable are configurable. No monetary or aggregate token budget is enforced. Usage comes from the provider and may be unavailable or partial.

## TUI

The left pane lists tasks using the full available height; the right pane shows the selected task's details and execution events. The sidebar scrolls to keep the selection visible and adapts when the terminal is resized. New-task selection opens in the right pane.

`n` creates a task by selecting pipeline, repository and provider. Arrow keys select a task; Home/End jump to the first/last task. `c` cancels it, `r` creates a new run using the currently loaded profiles, `p` retries publication or a failed source pull without generating again, and Page Up/Down scroll events in the right pane. `q` or Ctrl+C cancels active work, saves history and exits. Configuration is edited externally and loaded at startup.

History lives in the OS user configuration directory under `slop-generator`; override it with `-data-dir PATH`. Keep this directory private: it contains instructions and generated content, although API credentials and proxy URLs are excluded. Use one application instance per data directory. Interrupted tasks are not automatically resumed.

## Pipelines

Pipelines are implemented in Go. Each entry under `pipelines` chooses `python` or `text`, supplies the initial `instruction`, and embeds a `jobs` list with unique `id` and `instruction` fields. Text jobs may choose `.txt` (default) or `.md` through `extension`. Existing configurations using `jobs_file` must move their jobs into this list.

Python requests raw source, permits removal of one surrounding Markdown code fence, and checks syntax through Python's `py_compile` module in isolated interpreter mode. Bytecode goes to a separate cache with short filenames. A failed check is sent back to the model for up to three repairs. Generated programs are never executed; syntax validation does not establish correctness. Text requires a nonempty answer. API failures and empty answers stop the task.

Each task processes all jobs sequentially, saves them under `<output_dir>/<task_id>/`, and creates one commit after every job succeeds. Validation failures retain the private working copy and publish nothing.

## Git behavior

The application creates an independent clone from the selected **local branch** and verifies that the current remote branch is its ancestor before spending inference tokens. Local unpublished commits will also be included in the push. If the local branch is behind or diverged, synchronize it manually first.

After successful publication, `pull_after_push` updates the source checkout using `git pull --ff-only`. It defaults to `true`; set `pull_after_push: false` in a repository profile to leave the source checkout unchanged. The checkout must be on the configured branch. Pull uses the same push destination as the private copy, even if the remote has different fetch and push URLs. Automatic stash and rebase are disabled; Git preserves unrelated local changes and refuses updates that would overwrite files or require a merge. Bare source repositories require `pull_after_push: false`. Tasks sharing a source Git directory run sequentially through publication and pull.

If pull fails after push, the task records that the commit is already published and shows the pull error. The private copy is retained. Resolve the source checkout issue and press `p` to retry only pull, without regeneration or another push. Publication state survives application restarts. With pull disabled, update the source branch manually before running another task.

Only generated task files are staged. Git identity is copied from the source configuration, or supplied using `author_name` and `author_email`. Commit hooks are disabled in the private clone. Publication uses a normal fast-forward push with no force push, automatic rebase or merge. If another writer changes the branch, publication may fail; the commit and working copy are retained. Press `p` to retry without generating again. If branches have diverged, resolve the saved copy manually; its path is shown in task details.

When a push loses its connection, the application checks whether the remote contains the commit before reporting failure. Successful working copies are removed. Failed copies and Python bytecode caches remain in the data directory for manual cleanup.

## Development

```text
go test ./...
go vet ./...
go build -o slop-generator.exe ./cmd/slop-generator
```

Tests use a local HTTP server and temporary Git repositories; they do not spend inference tokens or contact a hosting service.

## Project structure

```text
cmd/slop-generator/   CLI flags and concrete dependency wiring
internal/config/      Single-file YAML loading, defaults and validation
internal/inference/   Request/result types and OpenAI-compatible HTTP client
internal/pipeline/    Fixed generation/validation sequences and Python checks
internal/gitrepo/     Git CLI adapter: prepare, commit and publish
internal/history/     JSON file storage
internal/task/        Task state, scheduling and orchestration
internal/tui/         Terminal presentation and user actions
tests/integration/    Explicitly enabled live API check
```

Interfaces are defined by their consumers. `pipeline.Generator` describes only inference; `task.Provider` adds client cleanup. `task.Pipeline` is the `Run(context.Context, pipeline.Runtime) error` contract implemented by `pipeline.Builtin`. `task.Repository` and `task.Store` describe the Git and history capabilities consumed by orchestration. `tui.Controller` exposes task actions, snapshots and profile names without exposing service internals.

`task.New` requires explicit dependencies. The command selects the HTTP client, built-in pipeline, Git adapter and JSON store; the task service does not instantiate them. Commit and push remain orchestration steps after a successful pipeline. Adding an implementation does not require changing the TUI or scheduler.

Service configuration and returned task snapshots are copied; snapshots and persisted history omit provider credentials. Existing YAML configuration and history files keep their formats.

The live test is optional and reads the `openrouter` profile from root `config.yaml`:

```powershell
$env:SLOP_LIVE = '1'
go test ./tests/integration -run TestLiveInference -v
```

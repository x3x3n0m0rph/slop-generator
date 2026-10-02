package stage

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// CheckPython validates syntax without executing generated code. Invalid syntax
// is returned as diagnostics and invalid=true; infrastructure failures are errors.
// Explicit cfile avoids Windows path limits caused by mirroring the full source
// path under PYTHONPYCACHEPREFIX. Isolated mode imports only Python's stdlib.
func CheckPython(ctx context.Context, python, source, cache string) (string, bool, error) {
	if err := os.MkdirAll(filepath.Dir(cache), 0700); err != nil {
		return "", false, err
	}
	check, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	script := `import py_compile, sys
try:
    py_compile.compile(sys.argv[1], cfile=sys.argv[2], doraise=True)
except py_compile.PyCompileError as exc:
    print(exc, file=sys.stderr)
    sys.exit(1)
except Exception:
    sys.exit(2)
`
	cmd := exec.CommandContext(check, python, "-I", "-c", script, source, cache)
	b, err := cmd.CombinedOutput()
	if err == nil {
		return "", false, nil
	}
	if ctx.Err() != nil {
		return "", false, ctx.Err()
	}
	if check.Err() != nil {
		return "", false, fmt.Errorf("Python check timed out")
	}
	exit, ok := err.(*exec.ExitError)
	if !ok {
		return "", false, fmt.Errorf("cannot start Python interpreter")
	}
	if exit.ExitCode() != 1 {
		return "", false, fmt.Errorf("Python validation failed to read source or write cache")
	}
	return string(b), true, nil
}

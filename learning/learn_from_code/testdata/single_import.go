// testdata/single_import.go — single-import-form fixture (dogfood finding #2,
// 2026-09-11): v1 regexes only saw the parenthesized block form and missed
// `import "os/exec"`.
package risky

import "os/exec"

func Shell(cmd string) error { return exec.Command(cmd).Run() }

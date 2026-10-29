# Git signing prompt reaching the terminal: reproduction

Recorded 2026-10-29 on macOS with git 2.50.1, OpenSSH 9.9p2 and Go 1.26.1.

Run the attached variant only inside an isolated pseudo-terminal (`script`, below). Started directly from an agent session, it reads the host terminal, which is the defect being reproduced.

## Setup

A throwaway repository signing with a passphrase-protected SSH key that is not in any agent:

```sh
S=$(mktemp -d)
ssh-keygen -q -t ed25519 -N 'secret-pass' -f "$S/key" -C repro
mkdir "$S/repo" && cd "$S/repo" && git init -q
git config user.name repro
git config user.email repro@example.invalid
git config gpg.format ssh
git config user.signingkey "$S/key"
git config commit.gpgsign true
echo a > f.txt && git add f.txt
```

## Program

Runs `git commit` the way `GitFinalizer` in `pkg/local/git_finalizer.go` does (`exec.CommandContext` plus `CombinedOutput`), optionally detached the way `runDetached` in `internal/git/git.go` does (`Setsid`). The deadline is 3s to keep the run short.

```go
package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"time"
)

func main() {
	repo, detach := os.Args[1], os.Args[2] == "detach"
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "-C", repo, "commit", "-m", "x", "--", "f.txt")
	cmd.Env = append(os.Environ(), "SSH_AUTH_SOCK=")
	if detach {
		cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	}
	start := time.Now()
	out, err := cmd.CombinedOutput()
	fmt.Printf("\n[detach=%v] returned after %.1fs err=%v ctxErr=%v\noutput: %q\n", detach, time.Since(start).Seconds(), err, ctx.Err(), out)
}
```

## Run

`script` gives the program its own pseudo-terminal as controlling terminal. `sleep 10` keeps that terminal's input open for 10s without typing anything:

```sh
sleep 10 | script -q /dev/null ./repro "$S/repo" attached
sleep 10 | script -q /dev/null ./repro "$S/repo" detach
```

## Results

Attached (as the finalizer runs today):

```
Enter passphrase for "<key>":
[detach=false] returned after 9.7s err=signal: killed ctxErr=context deadline exceeded
output: "error: Load key \"<key>\": incorrect passphrase supplied to decrypt private key?\n\nfatal: failed to write commit object\n"
```

- The prompt is written to the terminal, not to the captured output.
- The deadline passes at 3s and `git` is killed, but `CombinedOutput` returns only when the terminal input closes. `ssh-keygen` outlives `git` and keeps the output pipe open, so the context timeout does not limit the call.
- "incorrect passphrase" comes from whatever the signer read from the terminal (here end of input). In real use it is the user's keystrokes.

Detached:

```
[detach=true] returned after 0.0s err=exit status 128 ctxErr=<nil>
output: "error: Enter passphrase for \"<key>\": Load key \"<key>\": incorrect passphrase supplied to decrypt private key?\n\nfatal: failed to write commit object\n"
```

- Fails immediately. The prompt text and Git's error both land in the captured output, and the terminal is never touched.
- The message still reads "incorrect passphrase", although no passphrase was entered.

## Observed in use

This happened in a Claude Code session on another project, after a reboot left the signing key out of the SSH agent. The agent itself was reachable.

- An engine capture's commit prompted on the terminal Claude Code was reading.
- Claude Code received keyboard and mouse sequences without their escape bytes, such as `[119;5u` and `<35;116;47M`.
- The signer failed with "incorrect passphrase".
- The write took minutes to return.
- The terminal stayed garbled until Claude Code was quit and the terminal was reset.
- After `ssh-add`, the retry succeeded.

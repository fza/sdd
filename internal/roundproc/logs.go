package roundproc

import (
	"fmt"
	"path/filepath"
)

// LogDirName is the directory under sdd's state root holding the logs of
// round processes, one directory per repository.
const LogDirName = "rounds"

// LogPath is the log in dir of the process of round number of the delegate
// child session.
func LogPath(dir, session string, number int) string {
	return filepath.Join(dir, fmt.Sprintf("%s-round-%d.log", session, number))
}

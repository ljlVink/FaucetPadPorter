package utils

import(
	"os/exec"
	"os"
)

func RunCommand(dir string,command string, args ...string) error {
	cmd := exec.Command(command, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Dir = dir

	err := cmd.Run()
	if err != nil {
		return err
	}

	return nil
}

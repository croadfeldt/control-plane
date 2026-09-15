package main

import (
	"os"

	gitopsapp "github.com/dcm-project/control-plane/internal/gitops/app"
)

func main() {
	os.Exit(gitopsapp.Run())
}

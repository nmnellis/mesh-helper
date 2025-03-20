package main

import (
	"context"
	"fmt"
	"github.com/nmnellis/mesh-helper/cmd"
)

func main() {
	err := cmd.RootCommand(context.Background()).Execute()
	if err != nil {
		fmt.Print(err)
	}
}

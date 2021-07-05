package commands

import (
	"errors"
	"os"

	"github.com/urfave/cli/v2"

	"github.com/rupor-github/fb2converter/config"
	"github.com/rupor-github/fb2converter/state"
	"github.com/rupor-github/fb2converter/static"
)

// ExportResources is "export" command body.
func ExportResources(ctx *cli.Context) error {

	// var err error

	const (
		errPrefix = "export: "
		errCode   = 1
	)

	env := ctx.Generic(state.FlagName).(*state.LocalEnv)

	fname := ctx.Args().Get(0)
	if len(fname) == 0 {
		return cli.NewExitError(errors.New(errPrefix+"destination directory has not been specified"), errCode)
	}
	//nolint:gocritic
	if info, err := os.Stat(fname); err != nil && !os.IsNotExist(err) {
		return cli.NewExitError(errors.New(errPrefix+"unable to access destination directory"), errCode)
	} else if err != nil {
		return cli.NewExitError(errors.New(errPrefix+"destination directory does not exits"), errCode)
	} else if !info.IsDir() {
		return cli.NewExitError(errors.New(errPrefix+"destination is not a directory"), errCode)
	}

	ignoreNames := map[string]bool{
		config.DirHyphenator: true,
		config.DirResources:  true,
		config.DirSentences:  true,
	}

	if dir, err := static.AssetDir(""); err == nil {
		for _, a := range dir {
			if _, ignore := ignoreNames[a]; len(env.Debug) != 0 || !ignore {
				err = static.RestoreAssets(fname, a)
				if err != nil {
					return cli.NewExitError(errors.New(errPrefix+"unable to store resources"), errCode)
				}
			}
		}
	}
	return nil
}

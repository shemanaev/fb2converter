package main

import (
	"fmt"
	"os"
	"runtime"

	"go.uber.org/zap"

	"fb2converter/misc"
	"fb2converter/processor/kfx"
)

func main() {

	logger, _ := zap.NewDevelopment()
	log := logger.Sugar()
	defer log.Sync()

	usage := func() {

		fmt.Fprintf(os.Stderr, "\nKFX dumper\nVersion %s (%s) : %s\n\n",
			misc.GetVersion(),
			runtime.Version(),
			misc.GetGitHash())
		fmt.Fprintf(os.Stderr, "Usage: %s <kfx_file>\n\n", os.Args[0])
	}

	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}

	f, err := os.Open(os.Args[1])
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()

	var l int64
	if s, err := f.Stat(); err != nil {
		log.Fatal(err)
	} else {
		l = s.Size()
	}

	ctnr := kfx.NewContainer(logger)
	if err := ctnr.Load(f, l); err != nil {
		log.Fatal(err)
	}
	// c.Dump(os.Stdout)
}

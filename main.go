package main

import (
	"os"

	cli "github.com/jawher/mow.cli"
	"github.com/sirkon/goproxy/plugin/apriori"
	"github.com/sirkon/goproxy/plugin/cascade"
)

func envGoproxy() string {
	res := os.Getenv("GOPROXY")
	if len(res) == 0 {
		res = "https://proxy.golang.org"
	}
	return res
}

func main() {
	ap := &argsProcessor{
		logger: makeLogger(),
	}

	app := cli.App("apriori", "A go proxy with special care for pre-existing modules")
	useLegacy := app.BoolOpt("use-legacy", false, "Use legacy VCS module fetcher instead of go modules proxy")
	withLegacyRoot := app.StringOpt("with-root", "", "Use this directory for VCS plugin caching")
	useGoproxy := app.StringOpt("use-goproxy", envGoproxy(), "Use this goproxy to fetch modules")
	app.Spec = "[--use-legacy --with-root=<directory>|--use-goproxy=<goproxy URL>]" //[COMMAND [ARG...]]"

	app.Command("serve", "Serving go module proxy with given apriori file", ap.serving())
	app.Command("generate", "Generate apriori data for given modules", ap.generation())

	app.Before = func() {
		if *useLegacy {
			plug, err := apriori.NewPlugin(*withLegacyRoot)
			if err != nil {
				ap.logger.Fatal().Msgf("failed to initiate legacy logger: %s", err)
			}
			ap.plugin = plug
			ap.logger.Info().Msgf("using legacy module fetcher")
		} else {
			ap.plugin = cascade.NewPlugin(*useGoproxy)
			ap.logger.Info().Msgf("using %s go modules proxy as a source", *useGoproxy)
		}
	}

	if err := app.Run(os.Args); err != nil {
	}
}

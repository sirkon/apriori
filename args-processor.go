package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"syscall"
	"time"

	cli "github.com/jawher/mow.cli"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/sirkon/goproxy"
	"github.com/sirkon/goproxy/gomod"
	"github.com/sirkon/goproxy/plugin/apriori"
	"github.com/sirkon/goproxy/plugin/cascade"
	"github.com/sirkon/goproxy/plugin/choice"
	"github.com/sirkon/goproxy/plugin/vcs"
	"github.com/sirkon/goproxy/semver"

	"github.com/sirkon/apriori/internal/modinfo"
)

type argsProcessor struct {
	plugin goproxy.Plugin
	logger *zerolog.Logger
}

func (ap *argsProcessor) initLegacy(legacyRoot string) {
	plg, err := vcs.NewPlugin(legacyRoot)
	if err != nil {
		ap.logger.Fatal().Msgf("failed to initiate legacy plugin: %s", err)
	}
	ap.plugin = plg
}

func (ap *argsProcessor) initGoproxy(url string) {
	ap.plugin = cascade.NewPlugin(url)
}

func (ap *argsProcessor) serving() cli.CmdInitializer {
	return func(cmd *cli.Cmd) {
		listenAddr := cmd.StringOpt("listen", "0.0.0.0:7979", "Address to listen to")
		aprioriPath := cmd.StringOpt("apriori", "", "File to read apriori info from")
		cmd.Spec = "[--listen=<addr>] --apriori=<info.json>"

		cmd.Action = func() {
			plug, err := apriori.NewPlugin(*aprioriPath)
			if err != nil {
				ap.logger.Fatal().Msgf("failed to initiate apriori info plugin: %s", err)
			}
			plug = choice.New(plug, ap.plugin)

			r, err := goproxy.NewRouter()
			if err != nil {
				ap.logger.Fatal().Msgf("failed to initiate a routerL %s: %s", err)
			}
			if err := r.AddRoute("", plug); err != nil {
				ap.logger.Fatal().Msgf("failed to add default route: %s", err)
			}

			m := goproxy.Middleware(r, "", ap.logger)

			server := http.Server{
				Addr:    *listenAddr,
				Handler: m,
			}

			errCh := make(chan error)
			go func() {
				err := server.ListenAndServe()
				if err != nil {
					errCh <- err
				}
			}()

			ap.logger.Info().Msgf("serving apriori go modules proxy as  http://%s", *listenAddr)

			signCh := make(chan os.Signal)
			signal.Notify(signCh, os.Interrupt, syscall.SIGTERM)

			select {
			case err := <-errCh:
				log.Fatal().Err(err).Msg("exitting")
			case sign := <-signCh:
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				_ = server.Shutdown(ctx)
				ap.logger.Info().Str("signal", sign.String()).Msg("server stopped on signal")
			}

		}
	}
}

func (ap *argsProcessor) generation() cli.CmdInitializer {
	return func(cmd *cli.Cmd) {
		modSrc := cmd.StringOpt("source", "", "Modules list source. Omit this option to use stdin")
		gomodDir := cmd.StringOpt("gomod-dir", "", "Directory to save go.mod files in")
		srcDir := cmd.StringOpt("source-dir", "", "Directory so save source archives in")
		aprioriDest := cmd.StringOpt("dest", "", "File to save apriori info. Omit this option to use stdout")
		recursive := cmd.BoolOpt("r recursive", false, "Download dependecies as well")
		cmd.Spec = "[--recursive | -r] [--source=<source path>] [--dest=<apriori info file>] --gomod-dir=<go modules dir> --source-dir=<source archives dir>"
		cmd.Action = func() {
			if err := checkIfDir("invalid go modules directory (--gomod-dir)", *gomodDir); err != nil {
				ap.logger.Fatal().Msgf(err.Error())
			}
			if err := checkIfDir("invalid source directory (--source-dir)", *srcDir); err != nil {
				ap.logger.Fatal().Msgf(err.Error())
			}

			var src *os.File
			if len(*modSrc) == 0 {
				src = os.Stdin
			} else {
				file, err := os.Open(*modSrc)
				if err != nil {
					ap.logger.Fatal().Err(err).Msg("failed to open a source")
				}
				src = file
			}

			var dest *os.File
			if len(*aprioriDest) == 0 {
				dest = os.Stdout
			} else {
				file, err := os.Create(*aprioriDest)
				if err != nil {
					ap.logger.Fatal().Err(err).Msg("failed to open apriori destination")
				}
				dest = file
			}

			defer func() {
				if err := src.Close(); err != nil {
					ap.logger.Error().Err(err).Msg("failed to close source file")
				}
				if err := dest.Close(); err != nil {
					ap.logger.Error().Err(err).Msg("failed to close dest file")
				}
			}()

			mapping := apriori.Mapping{}
			if err := ap.createAprioriInfo(modinfo.NewChannelFromSource(src.Name(), src), *gomodDir, *srcDir, *recursive, mapping); err != nil {
				ap.logger.Fatal().Err(err).Msg("failed to generate apriori info")
			}

			data, err := json.MarshalIndent(mapping, "", "  ")
			if err != nil {
				ap.logger.Fatal().Err(err).Msg("failed to serialize apriori info")
			}
			if _, err := dest.Write(data); err != nil {
				ap.logger.Fatal().Err(err).Msg("failed to save apriori info")
			}
			ap.logger.Info().Msg("apriori generation done")
		}
	}
}

func (ap *argsProcessor) getModule(module string) (goproxy.Module, error) {
	// We don't actually care about proxy address, we just need *http.Request instance with certain module path
	req, _ := http.NewRequest("GET", fmt.Sprintf("http://proxy.golang.org/%s/@v/list", module), nil)
	ss, err := ap.plugin.Module(req, "")
	if err != nil {
		return nil, fmt.Errorf("failed to get a plugin for module %s", module)
	}
	return ss, nil
}

func (ap *argsProcessor) createAprioriInfo(src chan modinfo.Result, goModDir string, srcDir string, recursive bool, result apriori.Mapping) error {
	ctx := ap.logger.WithContext(context.Background())

	for item := range src {

		var mod goproxy.Module
		var version string
		var module string
		var err error

		switch v := item.(type) {
		case modinfo.Error:
			return v
		case modinfo.Module:
			module = v.Path
			version = v.Version
			mod, err = ap.getModule(v.Path)
			if err != nil {
				return err
			}
		case modinfo.Latest:
			module = v.Path
			mod, err = ap.getModule(v.Path)
			if err != nil {
				return err
			}
			if len(version) == 0 {
				versions, err := mod.Versions(ctx, "")
				if err != nil {
					return fmt.Errorf("failed to get versions for module %s: %s", module, err)
				}
				for _, vv := range versions {
					if !semver.IsValid(vv) {
						return fmt.Errorf("invalid semver value %s", vv)
					}
				}
				sort.Slice(versions, func(i, j int) bool {
					return semver.Compare(versions[i], versions[j]) < 0
				})
				version = versions[len(versions)-1]
			}
		}

		if versionsInfo, ok := result[module]; ok {
			if _, ok := versionsInfo[version]; ok {
				continue
			}
		}

		// getting revision info
		revInfo, err := mod.Stat(ctx, version)
		if err != nil {
			return fmt.Errorf("failed to get revision info for %s@%s: %s", module, version, err)
		}

		// getting go.mod
		gomodInfo, err := mod.GoMod(ctx, version)
		if err != nil {
			return fmt.Errorf("failed to get go.mod for %s@%s: %s", module, version, err)
		}

		// getting source archive
		archive, err := mod.Zip(ctx, version)
		if err != nil {
			return fmt.Errorf("failed to get source archive for %s@%s: %s", module, version, err)
		}

		gomodPath := filepath.Join(goModDir, module, version+".mod")
		srcPath := filepath.Join(srcDir, module, version+".zip")
		if err := ap.saveInfo(gomodInfo, gomodPath, archive, srcPath); err != nil {
			return fmt.Errorf("failed to save %s@%s info: %s", module, version, err)
		}

		moduleInfo, ok := result[module]
		if !ok {
			moduleInfo = map[string]apriori.ModuleInfo{}
		}
		moduleInfo[version] = apriori.ModuleInfo{
			RevInfo:     *revInfo,
			GoModPath:   gomodPath,
			ArchivePath: srcPath,
		}
		result[module] = moduleInfo
		ap.logger.Info().Msgf("%s@%s done", module, version)

		if recursive {
			goMod, err := gomod.Parse(gomodPath, gomodInfo)
			if err != nil {
				return fmt.Errorf("failed to parse dependency of %s: %s", module, err)
			}
			if err := ap.createAprioriInfo(modinfo.NewChannelFromGoMod(goMod), goModDir, srcDir, recursive, result); err != nil {
				return err
			}
		}
	}

	return nil
}

func (ap *argsProcessor) saveInfo(gomod []byte, gomodPath string, archive io.ReadCloser, srcPath string) error {
	defer func() {
		if err := archive.Close(); err != nil {
			ap.logger.Error().Err(err).Msg("failed to close archive reader")
		}
	}()

	base, _ := filepath.Split(gomodPath)
	if err := os.MkdirAll(base, 0755); err != nil {
		return fmt.Errorf("failed to save go.mod: %s", err)
	}
	if err := ioutil.WriteFile(gomodPath, gomod, 0644); err != nil {
		return fmt.Errorf("failed to save go.mod: %s", err)
	}

	base, _ = filepath.Split(srcPath)
	if err := os.MkdirAll(base, 0755); err != nil {
		return fmt.Errorf("failed to save source archive: %s", err)
	}
	file, err := os.Create(srcPath)
	if err != nil {
		return fmt.Errorf("failed to save source archive: %s", err)
	}
	defer func() {
		if err := file.Close(); err != nil {
			ap.logger.Error().Err(err).Msg("failed to close archive writer")
		}
	}()
	if _, err := io.Copy(file, archive); err != nil {
		return fmt.Errorf("failed to save source archive: %s", err)
	}

	return nil
}

func checkIfDir(errPrefix, path string) error {
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return fmt.Errorf("%s: directory `%s` does not exist", errPrefix, path)
	}
	if err != nil {
		return fmt.Errorf("%s: cannot access `%s`: %s", errPrefix, path, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("%s: `%s` is not a directory", errPrefix, path)
	}
	return nil
}

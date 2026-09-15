package cli

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/jsazanami-del/pg2protobuf-generator/internal/config"
	"github.com/jsazanami-del/pg2protobuf-generator/internal/engine"
)

func Execute(ctx context.Context) error {
	return NewRoot(ctx).ExecuteContext(ctx)
}

func NewRoot(ctx context.Context) *cobra.Command {
	var (
		conn        string
		schemas     []string
		out         string
		cfgPath     string
		lockFile    string
		dryRun      bool
		force       bool
		prune       bool
		exclude     []string
		strictTypes bool
		initForce   bool
		initCfg     string
	)

	root := &cobra.Command{
		Use:           "pg2proto",
		Short:         "Generate Protobuf messages from PostgreSQL catalogs",
		SilenceErrors: true,
		SilenceUsage:  true,
	}

	initCmd := &cobra.Command{
		Use:   "init",
		Short: "Write a .pg2proto.yaml template",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if initCfg == "" {
				initCfg = ".pg2proto.yaml"
			}
			if err := config.WriteFile(initCfg, config.InitTemplate(), initForce); err != nil {
				return errExit(ExitErrorCode, "%s", err.Error())
			}
			fmt.Fprintf(cmd.OutOrStdout(), "wrote %s\n", initCfg)
			return nil
		},
	}
	initCmd.Flags().StringVar(&initCfg, "config", ".pg2proto.yaml", "config file path")
	initCmd.Flags().BoolVar(&initForce, "force", false, "overwrite existing config")

	bindGen := func(cmd *cobra.Command) {
		cmd.Flags().StringVarP(&conn, "conn", "c", "", "PostgreSQL connection string (default $DATABASE_URL)")
		cmd.Flags().StringSliceVarP(&schemas, "schema", "s", []string{"public"}, "target schema(s)")
		cmd.Flags().StringVarP(&out, "out", "o", "./proto", "output directory for .proto files (overrides yaml proto.out)")
		cmd.Flags().StringVar(&cfgPath, "config", ".pg2proto.yaml", "config file")
		cmd.Flags().StringVar(&lockFile, "lock-file", ".pg2proto.lock", "lock file")
		cmd.Flags().StringSliceVar(&exclude, "exclude", nil, "exclude glob for relations and enums (added to yaml exclude)")
		cmd.Flags().BoolVar(&strictTypes, "strict-types", false, "error on unknown and composite types instead of google.protobuf.Any")
	}

	genCmd := &cobra.Command{
		Use:   "generate",
		Short: "Generate .proto files and update the lock file",
		RunE: func(cmd *cobra.Command, _ []string) error {
			opt := engine.Options{
				Conn:           conn,
				Schemas:        schemas,
				Out:            out,
				OutSet:         cmd.Flags().Changed("out"),
				Config:         cfgPath,
				LockFile:       lockFile,
				DryRun:         dryRun,
				Force:          force,
				Prune:          prune,
				Exclude:        exclude,
				StrictTypes:    strictTypes,
				StrictTypesSet: cmd.Flags().Changed("strict-types"),
				Stdout:         cmd.OutOrStdout(),
			}
			res, err := engine.Run(ctx, opt)
			if err != nil {
				return errExit(ExitErrorCode, "%s", err.Error())
			}
			if len(res.Breaking) > 0 && !force {
				return errExit(ExitIncompatible, "%s", strings.Join(res.Breaking, "\n"))
			}
			if err := engine.Write(opt, res); err != nil {
				return errExit(ExitErrorCode, "%s", err.Error())
			}
			if len(res.Breaking) > 0 && force {
				fmt.Fprintln(os.Stderr, "warning: wrote output despite incompatible changes:")
				fmt.Fprintln(os.Stderr, strings.Join(res.Breaking, "\n"))
			}
			return nil
		},
	}
	bindGen(genCmd)
	genCmd.Flags().BoolVar(&dryRun, "dry-run", false, "print generated files without writing")
	genCmd.Flags().BoolVar(&force, "force", false, "write even if changes are wire-incompatible")
	genCmd.Flags().BoolVar(&prune, "prune", false, "delete generated proto files for dropped relations/enums")

	checkCmd := &cobra.Command{
		Use:   "check",
		Short: "Fail if the schema is wire-incompatible with the lock file",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if _, err := os.Stat(lockFile); err != nil {
				if os.IsNotExist(err) {
					return errExit(ExitErrorCode, "lock file %s not found", lockFile)
				}
				return errExit(ExitErrorCode, "%s", err.Error())
			}
			opt := engine.Options{
				Conn:           conn,
				Schemas:        schemas,
				Out:            out,
				OutSet:         cmd.Flags().Changed("out"),
				Config:         cfgPath,
				LockFile:       lockFile,
				Exclude:        exclude,
				StrictTypes:    strictTypes,
				StrictTypesSet: cmd.Flags().Changed("strict-types"),
			}
			res, err := engine.Run(ctx, opt)
			if err != nil {
				return errExit(ExitErrorCode, "%s", err.Error())
			}
			if len(res.Breaking) > 0 {
				return errExit(ExitIncompatible, "%s", strings.Join(res.Breaking, "\n"))
			}
			fmt.Fprintln(cmd.OutOrStdout(), "ok")
			return nil
		},
	}
	bindGen(checkCmd)

	root.AddCommand(initCmd, genCmd, checkCmd)
	return root
}

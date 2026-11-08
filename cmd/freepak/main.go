// Command freepak builds a freely licensed replacement for id Software's
// shareware id1/pak0.pak from LibreQuake plus content generated here.
package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/go-quake1/freepak"
)

// osExit is replaced by tests.
var osExit = os.Exit

func main() {
	if err := RootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "freepak:", err)
		osExit(1)
	}
}

// RootCmd returns the freepak command tree.
func RootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "freepak",
		Short:         "Build a free replacement for id1/pak0.pak",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.AddCommand(buildCmd(), mapsCmd(), layoutCmd(), structureCmd())
	return root
}

func mapsCmd() *cobra.Command {
	var lqPath string
	c := &cobra.Command{
		Use:   "maps --librequake <lite.zip>",
		Short: "List the LibreQuake maps with their title, start, counts and changelevel targets",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			lq, err := freepak.OpenLibreQuake(lqPath)
			if err != nil {
				return err
			}
			infos, err := freepak.InspectMaps(lq.Files)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			for _, m := range infos {
				fmt.Fprintf(out, "%-14s start=%-5v monsters=%-3d items=%-3d size=%-8d exits=%v title=%q\n",
					m.Name, m.Start, m.Monsters, m.Items, m.Size, m.Exits, m.Title)
			}
			return nil
		},
	}
	c.Flags().StringVar(&lqPath, "librequake", "", "path to LibreQuake lite.zip (pinned by sha256)")
	_ = c.MarkFlagRequired("librequake")
	return c
}

func buildCmd() *cobra.Command {
	var opt freepak.Options
	c := &cobra.Command{
		Use:   "build (--reference <id pak0> | --no-reference) --librequake <lite.zip> --out <dir>",
		Short: "Build pak0.pak, MANIFEST and the licence files into a directory",
		Long: `Build the free pak0.pak in the layout of id's shareware pak0.pak.

--reference reads id's pak only for its paths and to prove that no byte run of
it is in the output; its content is never written. Without it, say
--no-reference: the MANIFEST then states that the reference gates did not run.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			res, err := freepak.Build(opt, cmd.ErrOrStderr())
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s  pak0.pak (%d entries)\n", res.PakSHA256, len(res.Entries))
			return nil
		},
	}
	f := c.Flags()
	f.StringVar(&opt.LibreQuake, "librequake", "", "path to LibreQuake lite.zip (pinned by sha256)")
	f.StringVar(&opt.Reference, "reference", "", "path to id's pak0.pak (paths and no-copy gates only)")
	f.BoolVar(&opt.NoReference, "no-reference", false, "build without the reference gates (stamped in the MANIFEST)")
	f.StringVar(&opt.OutDir, "out", "", "output directory")
	_ = c.MarkFlagRequired("librequake")
	_ = c.MarkFlagRequired("out")
	return c
}

func structureCmd() *cobra.Command {
	var ref string
	c := &cobra.Command{
		Use:   "structure --reference <id pak0>",
		Short: "Print the palette and colormap structure of a reference pak (no colour values)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			raw, err := os.ReadFile(ref)
			if err != nil {
				return err
			}
			files, err := freepak.ReadPak(raw)
			if err != nil {
				return err
			}
			s, err := freepak.StructureReport(files)
			if err != nil {
				return err
			}
			fmt.Fprint(cmd.OutOrStdout(), s)
			return nil
		},
	}
	c.Flags().StringVar(&ref, "reference", "", "path to id's pak0.pak")
	_ = c.MarkFlagRequired("reference")
	return c
}

func layoutCmd() *cobra.Command {
	var ref, out string
	c := &cobra.Command{
		Use:   "layout --reference <id pak0> --out layout.txt",
		Short: "Write the reference's paths and sizes (no content) to layout.txt",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			raw, err := os.ReadFile(ref)
			if err != nil {
				return err
			}
			files, err := freepak.ReadPak(raw)
			if err != nil {
				return err
			}
			return os.WriteFile(out, freepak.FormatLayout(files), 0o644)
		},
	}
	c.Flags().StringVar(&ref, "reference", "", "path to id's pak0.pak")
	c.Flags().StringVar(&out, "out", "", "file to write")
	_ = c.MarkFlagRequired("reference")
	_ = c.MarkFlagRequired("out")
	return c
}

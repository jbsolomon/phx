package gen

import (
	"fmt"
	"os"
	"runtime"
	"text/tabwriter"

	"github.com/phoenix-engine/phx/fs"
	"github.com/phoenix-engine/phx/gen/compress"
	"github.com/phoenix-engine/phx/gen/cpp"
	"github.com/phoenix-engine/phx/path"

	"github.com/pkg/errors"
)

// Gen uses Operate to process files in the FS given as From, and copies
// its output to To after processing is completed successfully.  It only
// operates on names matched by the Matcher.  It uses a temporary buffer
// for staging before completion.
type Gen struct {
	From, To fs.FS
	compress.Level

	SkipFinalize bool

	path.Matcher
	// TODO: Verbosity
}

// Operate processes files as in the description of the type.
func (g Gen) Operate() error {
	// TODO: Describe pipelines with a graph file.
	// TODO: Generate and check resource manifest for changes.
	prefis, err := g.From.ReadDir("")
	if err != nil {
		return errors.Wrapf(err, "reading %s", g.From)
	}

	var fis []os.FileInfo
	for _, fi := range prefis {
		if g.Match(fi.Name()) {
			fis = append(fis, fi)
		}
	}

	// In workers, open each file, zip and translate it into a
	// static array, and close it.  When each is done, it should be
	// in the tmp destination.  After they're all done, move them
	// all into the target destination.

	var (
		jobs, dones, kill, errs = MakeChans()

		tmpFS   = fs.MakeSyncMem()
		maker   = compress.LZ4Maker{Level: g.Level}
		encoder = cpp.PrepareTarget(tmpFS, maker)
	)

	for i := 0; i < runtime.NumCPU(); i++ {
		// TODO: Use real tmpdir for very large resources.
		// TODO: Figure out how to manage large / complicated
		// deps, such as git repos
		go Work{
			from:    g.From,
			Jobs:    jobs,
			Done:    dones,
			Kill:    kill,
			Errs:    errs,
			Encoder: encoder,
		}.Run()
	}

	go func() {
		for _, fi := range fis {
			// TODO: Check for nested resource dirs.
			// if fi.IsDir() { ... }
			jobs <- Job{Name: fi.Name()}
		}

		close(jobs)
	}()

	tw := new(tabwriter.Writer)
	tw.Init(os.Stdout, 0, 8, 0, '\t', 0)

	for i := 0; i < len(fis); i++ {
		select {
		case err := <-errs:
			close(kill)
			return err

		case d := <-dones:
			sizeStr := renderSize(d.Size)
			if cs := d.CompressedSize; cs != 0 {
				sizeStr += fmt.Sprintf(
					" / %s compressed (%.2f%%)",
					renderSize(cs),
					100*(1-(float64(cs)/float64(d.Size))),
				)
			}
			fmt.Fprintf(tw, "%s:\t%s\n", d.Name, sizeStr)
		}
	}

	tw.Flush()

	if !g.SkipFinalize {
		// Do any last synchronous cleanup the Encoder requires.
		if err := encoder.Finalize(); err != nil {
			return errors.Wrap(err, "finalizing Encoder")
		}
	}

	// All finished tmpfiles are now in the tmp destination and
	// shall be moved over to the target.

	tmpFis, err := tmpFS.ReadDir("")
	if err != nil {
		return errors.Wrap(err, "reading tempdir")
	}

	// TODO: Make this concurrent.
	for _, fi := range tmpFis {
		name := fi.Name()
		if err := fs.Move(tmpFS, g.To, name, name); err != nil {
			return errors.Wrapf(err, "finalizing %s", name)
		}
	}

	// TODO: If we used a real tmpdir, remove it now.
	return nil
}

// Size constants.
const (
	KB = 2 << 9
	MB = 2 << 19
	GB = 2 << 29
)

func renderSize(byteLen int64) string {
	switch {
	case byteLen < KB:
		return fmt.Sprintf("%d B", byteLen)
	case byteLen < MB:
		return fmt.Sprintf("%.2f KB", float64(byteLen)/KB)
	case byteLen < GB:
		return fmt.Sprintf("%.2f MB", float64(byteLen)/MB)
	default:
		return fmt.Sprintf("%.2f GB", float64(byteLen)/GB)
	}
}

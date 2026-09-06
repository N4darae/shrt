package catalog

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

type BuildSpec struct {
	Input  string
	Output string
	Binary string
}

func Build(ctx context.Context, spec BuildSpec) error {
	if spec.Input == "" {
		return fmt.Errorf("descriptor source input is empty")
	}
	if spec.Output == "" {
		return fmt.Errorf("descriptor output path is empty")
	}
	if err := os.MkdirAll(filepath.Dir(spec.Output), 0o755); err != nil {
		return err
	}
	bin := spec.Binary
	if bin == "" {
		bin = "buf"
	}
	if _, err := exec.LookPath(bin); err != nil {
		return fmt.Errorf("%q is not on PATH, and shrt builds the descriptor by running it: %w\n"+
			"shrt speaks two command lines, buf's and protoc's, chosen by descriptor.binary — install either, "+
			"or skip the build entirely by pointing descriptor.file at a descriptor set your own toolchain "+
			"already produces (any FileDescriptorSet will do)", bin, err)
	}
	args, err := buildArgs(bin, spec)
	if err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, bin, args...)
	out, runErr := cmd.CombinedOutput()
	if runErr != nil {
		return fmt.Errorf("%s %s: %w\n%s\n"+
			"%s ran but refused the input. descriptor.source is %q — for buf that must be the directory "+
			"holding your buf.yaml; for protoc it is the import root the .proto files sit under",
			bin, strings.Join(args, " "), runErr, out, bin, spec.Input)
	}
	return nil
}

func UsesProtoc(binary string) bool {
	base := strings.ToLower(filepath.Base(binary))
	return strings.HasPrefix(base, "protoc") && !strings.HasPrefix(base, "protoc-gen")
}

func buildArgs(bin string, spec BuildSpec) ([]string, error) {
	if !UsesProtoc(bin) {
		return []string{"build", spec.Input, "--as-file-descriptor-set", "-o", spec.Output}, nil
	}
	files, err := protoFiles(spec.Input)
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("no .proto files under %q, so protoc has nothing to compile — "+
			"descriptor.source must be the import root your protos sit under", spec.Input)
	}
	args := []string{
		"--descriptor_set_out=" + spec.Output,
		"--include_imports",
		"--include_source_info",
		"-I", spec.Input,
	}
	return append(args, files...), nil
}

func protoFiles(root string) ([]string, error) {
	out := []string{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if name := d.Name(); name != "." && (strings.HasPrefix(name, ".") || name == "node_modules" || name == "vendor") {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(path, ".proto") {
			out = append(out, path)
		}
		return nil
	})
	sort.Strings(out)
	return out, err
}

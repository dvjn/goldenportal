// Package cli is the goldport command line: it validates definition files
// offline and exports and applies definitions on a running portal.
package cli

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/dvjn/goldport/internal/definition"
)

const usage = `Usage:
  goldport                                       run the portal server
  goldport validate -f <file or directory>       check definition files offline
  goldport export -o <file>                      write the portal's definitions to a file, - for stdout
  goldport apply -f <file or directory> [--yes]  plan the files as the whole set of definitions, then apply

export and apply call the portal at GOLDPORT_URL with the service token in
GOLDPORT_TOKEN. apply needs a Maintainer token.
`

var errUsage = errors.New("usage")

// IO is what a command reads and writes, and its environment.
type IO struct {
	In     io.Reader
	Out    io.Writer
	Err    io.Writer
	Getenv func(string) string
}

// Run runs the command the arguments name and returns its exit code.
func Run(args []string, streams IO) int {
	commands := map[string]func([]string, IO) error{
		"validate": validate,
		"export":   export,
		"apply":    apply,
	}
	command, ok := commands[args[0]]
	if !ok {
		fmt.Fprint(streams.Err, usage)
		return 2
	}
	err := command(args[1:], streams)
	switch {
	case errors.Is(err, errUsage) || errors.Is(err, flag.ErrHelp):
		fmt.Fprint(streams.Err, usage)
		return 2
	case err != nil:
		fmt.Fprintln(streams.Err, err)
		return 1
	}

	return 0
}

func flags(name string, streams IO) *flag.FlagSet {
	set := flag.NewFlagSet(name, flag.ContinueOnError)
	set.SetOutput(streams.Err)

	return set
}

func validate(args []string, streams IO) error {
	set := flags("validate", streams)
	path := set.String("f", "", "a definition file or a directory of them")
	if err := set.Parse(args); err != nil {
		return err
	}
	if *path == "" {
		return errUsage
	}
	files, err := load(*path)
	if err != nil {
		return err
	}
	fmt.Fprintf(streams.Out, "%s in %s, all valid\n", count(len(files.set().Documents), "definition"), count(len(files), "file"))

	return nil
}

// file is one definition file, parsed.
type file struct {
	path   string
	text   []byte
	source definition.Source
}

type files []file

func (f files) set() definition.Set {
	var documents []definition.Document
	for _, file := range f {
		documents = append(documents, file.source.Documents...)
	}

	return definition.NewSet(documents...)
}

// text joins the files into one source of documents.
func (f files) text() string {
	parts := make([]string, 0, len(f))
	for _, file := range f {
		parts = append(parts, strings.TrimSuffix(string(file.text), "\n")+"\n")
	}

	return strings.Join(parts, "---\n")
}

// load reads the file, or every YAML and JSON file in the directory and its
// subdirectories, and fails with every problem found, each at its file and
// line.
func load(path string) (files, error) {
	paths, err := definitionPaths(path)
	if err != nil {
		return nil, err
	}
	var (
		loaded   files
		problems []string
		defined  = map[definition.Ref]string{}
	)
	for _, path := range paths {
		text, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		source, found := definition.Parse(text)
		for _, problem := range found {
			problems = append(problems, at(path, problem))
		}
		for _, document := range source.Documents {
			ref := document.Ref()
			if earlier, ok := defined[ref]; ok {
				duplicate := definition.Problem{Kind: ref.Kind, Identifier: ref.Identifier, Message: "also defined in " + earlier}
				problems = append(problems, at(path, source.Locate([]definition.Problem{duplicate})[0]))
			}
			defined[ref] = path
		}
		loaded = append(loaded, file{path: path, text: text, source: source})
	}
	if len(problems) == 0 {
		for _, problem := range loaded.set().Check() {
			problems = append(problems, loaded.locate(problem))
		}
	}
	if len(problems) > 0 {
		return nil, fmt.Errorf("%s\nfound %s, fix them and run again", strings.Join(problems, "\n"), count(len(problems), "problem"))
	}

	return loaded, nil
}

func (f files) locate(problem definition.Problem) string {
	for _, file := range f {
		if located := file.source.Locate([]definition.Problem{problem})[0]; located.Document > 0 {
			return at(file.path, located)
		}
	}

	return problem.String()
}

// at writes where a problem is the way compilers do, path:line:column.
func at(path string, problem definition.Problem) string {
	if problem.Line == 0 {
		return path + ": " + problem.String()
	}

	return fmt.Sprintf("%s:%d:%d: %s", path, problem.Line, problem.Column, problem)
}

func count(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}

	return fmt.Sprintf("%d %ss", n, noun)
}

func definitionPaths(path string) ([]string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return []string{path}, nil
	}
	var paths []string
	err = filepath.WalkDir(path, func(path string, entry os.DirEntry, err error) error {
		if err == nil && !entry.IsDir() && slices.Contains([]string{".yaml", ".yml", ".json"}, filepath.Ext(path)) {
			paths = append(paths, path)
		}
		return err
	})
	if err == nil && len(paths) == 0 {
		err = fmt.Errorf("%s holds no .yaml, .yml or .json files", path)
	}

	return paths, err
}

func export(args []string, streams IO) error {
	set := flags("export", streams)
	output := set.String("o", "", "the file to write, - for stdout")
	if err := set.Parse(args); err != nil {
		return err
	}
	if *output == "" {
		return errUsage
	}
	client, err := newClient(streams.Getenv)
	if err != nil {
		return err
	}
	var exported struct {
		Revision int    `json:"revision"`
		YAML     string `json:"yaml"`
	}
	if err := client.call("GET", "definitions/export", nil, &exported); err != nil {
		return err
	}
	if *output == "-" {
		_, err := io.WriteString(streams.Out, exported.YAML)
		return err
	}
	if err := os.WriteFile(*output, []byte(exported.YAML), 0o600); err != nil {
		return err
	}
	fmt.Fprintf(streams.Err, "Wrote revision %d to %s\n", exported.Revision, *output)

	return nil
}

func apply(args []string, streams IO) error {
	set := flags("apply", streams)
	path := set.String("f", "", "a definition file or a directory of them")
	yes := set.Bool("yes", false, "apply without asking")
	message := set.String("m", "", "a message for the revision")
	if err := set.Parse(args); err != nil {
		return err
	}
	if *path == "" {
		return errUsage
	}
	loaded, err := load(*path)
	if err != nil {
		return err
	}
	client, err := newClient(streams.Getenv)
	if err != nil {
		return err
	}
	var planned plan
	if err := client.call("POST", "definitions/plan", map[string]any{"source": loaded.text()}, &planned); err != nil {
		return err
	}
	planned.print(streams.Out)
	if planned.ID == "" {
		return nil
	}
	if !*yes && !confirmed(streams) {
		fmt.Fprintln(streams.Out, "Nothing applied.")
		return nil
	}
	var revision struct {
		Number int `json:"number"`
	}
	if err := client.call("POST", "definitions/apply", map[string]any{"plan_id": planned.ID, "message": *message}, &revision); err != nil {
		return err
	}
	fmt.Fprintf(streams.Out, "Applied revision %d.\n", revision.Number)

	return nil
}

func confirmed(streams IO) bool {
	fmt.Fprint(streams.Out, "Apply this plan? Type yes to apply: ")
	answer, _ := bufio.NewReader(streams.In).ReadString('\n')

	return strings.TrimSpace(answer) == "yes"
}

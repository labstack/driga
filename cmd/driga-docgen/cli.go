package main

import (
	"fmt"
	"io"
	"strings"

	"github.com/alecthomas/kong"

	"github.com/labstack/driga/internal/cli"
)

// cliModel builds the same Kong model `driga --help` prints from. Nothing is
// parsed or run; the model is only walked.
func cliModel() (*kong.Application, error) {
	root := new(cli.Root)
	parser, err := cli.NewParser(root,
		kong.Vars{"version": "driga (documentation)"},
		kong.Writers(io.Discard, io.Discard),
		kong.Exit(func(int) {}),
	)
	if err != nil {
		return nil, err
	}
	return parser.Model, nil
}

func renderCLI() (string, error) {
	app, err := cliModel()
	if err != nil {
		return "", err
	}
	var b strings.Builder
	frontmatter(&b,
		"CLI reference",
		"Every driga command, subcommand, argument and flag, with defaults and allowed values.",
		"Every visible driga command and flag, generated from the same Kong command tree that prints `driga --help`.",
		"You need the exact spelling, default or allowed values of a driga flag.",
		"You are scripting driga and want to know which commands take --json or --yes.",
	)
	fmt.Fprintf(&b, "%s\n\n", text(app.Help))
	b.WriteString("This page is generated from the command tree that `driga --help` prints, so it\n")
	b.WriteString("matches the binary built from the same commit. Run `driga <command> --help` for\n")
	b.WriteString("the same text in a terminal.\n\n")
	if app.DefaultCmd != nil {
		fmt.Fprintf(&b, "Running `driga` with no command runs `driga %s`.\n\n", app.DefaultCmd.Name)
	}
	b.WriteString("Exit codes: `0` success; `1` failure, refusal or partial completion; `2` invalid\n")
	b.WriteString("usage or missing required non-interactive input. A command that needs\n")
	b.WriteString("confirmation exits `2` when stdin is not a terminal unless `--yes` is passed.\n\n")

	// Application-level flags (--version, --help) apply everywhere.
	if flags := visibleFlags(app.Flags); len(flags) > 0 {
		b.WriteString("## Global flags\n\n")
		flagTable(&b, flags)
	}

	b.WriteString("## Commands\n\n")
	b.WriteString("| Command | Description |\n|---|---|\n")
	for _, node := range visibleChildren(app.Node) {
		fmt.Fprintf(&b, "| [%s](#%s) | %s |\n", code(node.FullPath()), anchor(node), cell(node.Help))
	}
	b.WriteString("\n")

	for _, node := range visibleChildren(app.Node) {
		renderCommand(&b, node, 2)
	}
	return strings.TrimRight(b.String(), "\n") + "\n", nil
}

func renderCommand(b *strings.Builder, node *kong.Node, depth int) {
	fmt.Fprintf(b, "%s %s\n\n", strings.Repeat("#", depth), node.FullPath())
	if node.Help != "" {
		fmt.Fprintf(b, "%s\n\n", text(sentence(node.Help)))
	}
	if node.Detail != "" && node.Detail != node.Help {
		fmt.Fprintf(b, "```text\n%s\n```\n\n", strings.TrimSpace(node.Detail))
	}
	children := visibleChildren(node)
	if len(children) == 0 || len(node.Positional) > 0 || len(visibleFlags(node.Flags)) > 0 {
		fmt.Fprintf(b, "```sh\n%s\n```\n\n", usage(node))
	}
	if node.DefaultCmd != nil {
		fmt.Fprintf(b, "Without a subcommand, runs `%s`.\n\n", node.DefaultCmd.FullPath())
	}
	if len(node.Positional) > 0 {
		b.WriteString("| Argument | Default | Description |\n|---|---|---|\n")
		for _, arg := range node.Positional {
			name := "<" + arg.Name + ">"
			if arg.IsCumulative() {
				name += " ..."
			}
			if !arg.Required {
				name = "[" + name + "]"
			}
			fmt.Fprintf(b, "| %s | %s | %s |\n", code(name), defaultCell(arg), cell(describe(arg)))
		}
		b.WriteString("\n")
	}
	if flags := visibleFlags(node.Flags); len(flags) > 0 {
		flagTable(b, flags)
	}
	if len(children) > 0 {
		b.WriteString("| Subcommand | Description |\n|---|---|\n")
		for _, child := range children {
			fmt.Fprintf(b, "| [%s](#%s) | %s |\n", code(child.FullPath()), anchor(child), cell(child.Help))
		}
		b.WriteString("\n")
		for _, child := range children {
			renderCommand(b, child, min(depth+1, 4))
		}
	}
}

func flagTable(b *strings.Builder, flags []*kong.Flag) {
	b.WriteString("| Flag | Default | Description |\n|---|---|---|\n")
	for _, flag := range flags {
		fmt.Fprintf(b, "| %s | %s | %s |\n", code(flagName(flag)), defaultCell(flag.Value), cell(describe(flag.Value)))
	}
	b.WriteString("\n")
}

func flagName(flag *kong.Flag) string {
	name := "--" + flag.Name
	if flag.Negated {
		name = "--[no-]" + flag.Name
	}
	if !flag.IsBool() && !flag.IsCounter() {
		placeholder := flag.PlaceHolder
		if placeholder == "" {
			placeholder = strings.ToLower(flag.Name)
		}
		name += "=<" + placeholder + ">"
	}
	if flag.Short != 0 {
		name = fmt.Sprintf("-%c, %s", flag.Short, name)
	}
	return name
}

func describe(v *kong.Value) string {
	help := sentence(v.Help)
	if v.Enum != "" && !strings.Contains(v.OrigHelp, "${enum}") && !mentionsEvery(v.Help, v.EnumSlice()) {
		values := v.EnumSlice()
		for i, value := range values {
			values[i] = "`" + value + "`"
		}
		help = strings.TrimSpace(help + " One of " + strings.Join(values, ", ") + ".")
	}
	if v.Required && v.Flag != nil {
		help = strings.TrimSpace(help + " Required.")
	}
	return help
}

func defaultCell(v *kong.Value) string {
	if !v.HasDefault || v.Default == "" {
		return ""
	}
	return code(v.Default)
}

func usage(node *kong.Node) string {
	parts := []string{node.FullPath()}
	for _, arg := range node.Positional {
		s := "<" + arg.Name + ">"
		if arg.IsCumulative() {
			s += "..."
		}
		if !arg.Required {
			s = "[" + s + "]"
		}
		parts = append(parts, s)
	}
	if len(visibleFlags(node.Flags)) > 0 {
		parts = append(parts, "[flags]")
	}
	return strings.Join(parts, " ")
}

func visibleChildren(node *kong.Node) []*kong.Node {
	var out []*kong.Node
	for _, child := range node.Children {
		if child.Hidden || child.Type != kong.CommandNode {
			continue
		}
		out = append(out, child)
	}
	return out
}

func visibleFlags(flags []*kong.Flag) []*kong.Flag {
	var out []*kong.Flag
	for _, flag := range flags {
		if flag.Hidden {
			continue
		}
		out = append(out, flag)
	}
	return out
}

// anchor reproduces the heading slug Starlight generates for a command heading
// ("driga fleet peers add" -> "driga-fleet-peers-add").
func anchor(node *kong.Node) string {
	return strings.ReplaceAll(strings.ToLower(node.FullPath()), " ", "-")
}

func sentence(s string) string {
	s = strings.TrimSpace(s)
	if s == "" || strings.HasSuffix(s, ".") || strings.HasSuffix(s, "?") || strings.HasSuffix(s, "!") {
		return s
	}
	return s + "."
}

// mentionsEvery reports whether help already names every allowed value.
func mentionsEvery(help string, values []string) bool {
	for _, value := range values {
		if !strings.Contains(help, value) {
			return false
		}
	}
	return true
}

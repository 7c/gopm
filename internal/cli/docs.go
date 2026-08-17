package cli

// docs.go implements `gopm docs`, the complete capability reference for gopm.
// It exists so that an AI agent (or a new operator) can run one command and
// learn every subcommand, flag, file format, exit code, and automation recipe
// without reading the source or the README.
//
// ┌───────────────────────────────────────────────────────────────────────────┐
// │ MAINTENANCE CONTRACT — READ THIS BEFORE CHANGING ANY COMMAND              │
// │                                                                           │
// │ Every new command, flag, config key, exit code, status value, or          │
// │ behavior change MUST be reflected in the docs topics in the SAME commit.  │
// │ Documentation that lags the binary is worse than none: agents act on it.  │
// │                                                                           │
// │ Where to edit:                                                            │
// │   docs_guides.go            cross-cutting topics (config, ecosystem, ...) │
// │   docs_commands_process.go  one docTopic per process-management command   │
// │   docs_commands_daemon.go   one docTopic per daemon-management command    │
// │   docs_commands_tools.go    config/state and tool commands                │
// │                                                                           │
// │ docs_test.go fails when a registered command or one of its flags has no   │
// │ matching docTopic entry, so the test suite enforces this contract.        │
// └───────────────────────────────────────────────────────────────────────────┘

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/7c/gopm/internal/display"
	"github.com/spf13/cobra"
)

// docsWidth is the wrap column for prose. It is fixed rather than derived from
// the terminal so that piping `gopm docs` to a file or an agent produces
// byte-identical output regardless of where it ran.
const docsWidth = 80

// docsMinDescWidth keeps flag descriptions readable when a long flag spec eats
// most of the line: past this point the description overruns docsWidth rather
// than being wrapped into a two-word-per-line column.
const docsMinDescWidth = 32

// Topic group names. They mirror the `gopm --help` sections so a command found
// in help is looked up under the same heading here.
const (
	docGroupConcepts = "Concepts & Formats"
	docGroupProcess  = "Process Management"
	docGroupDaemon   = "Daemon Management"
	docGroupConfig   = "Configuration & State"
	docGroupTool     = "Tools & Diagnostics"
)

// docFlag documents one flag. The parts are kept separate rather than
// pre-formatted so docs_test.go can compare Long against cobra's flag set.
type docFlag struct {
	Long    string `json:"long"`              // "--name", including dashes
	Short   string `json:"short,omitempty"`   // "-n", empty when there is no shorthand
	Arg     string `json:"arg,omitempty"`     // value placeholder, empty for booleans
	Default string `json:"default,omitempty"` // rendered default, empty when there is none
	Desc    string `json:"desc"`
}

// docExample is one runnable command line plus the reason to run it.
type docExample struct {
	Desc string `json:"desc"`
	Cmd  string `json:"cmd"`
}

// docTopic is one addressable section of the reference.
type docTopic struct {
	Name     string       `json:"name"`              // topic key: `gopm docs <name>`
	Group    string       `json:"group"`             // one of the docGroup* constants
	Command  string       `json:"command,omitempty"` // cobra command documented, "" for guides
	Summary  string       `json:"summary"`
	Body     []string     `json:"body,omitempty"` // paragraphs, wrapped on render
	Usage    []string     `json:"usage,omitempty"`
	Aliases  []string     `json:"aliases,omitempty"`
	Flags    []docFlag    `json:"flags,omitempty"`
	Examples []docExample `json:"examples,omitempty"`
	Notes    []string     `json:"notes,omitempty"`
	SeeAlso  []string     `json:"see_also,omitempty"`
}

// docTopics returns the full reference in render order: concepts first (an
// agent reading top to bottom learns the model before the commands), then the
// commands grouped exactly as `gopm --help` groups them.
func docTopics() []docTopic {
	groups := [][]docTopic{
		docGuideTopics(),
		docProcessCommandTopics(),
		docDaemonCommandTopics(),
		docStateCommandTopics(),
		docToolCommandTopics(),
	}
	var all []docTopic
	for _, g := range groups {
		all = append(all, g...)
	}
	return all
}

var (
	docsColorMode string
	docsListOnly  bool
)

var docsCmd = &cobra.Command{
	Use:   "docs [topic...]",
	Short: "Print the complete gopm reference (agent-friendly)",
	Long: `Print the complete gopm reference: every command, every flag, the file formats,
exit codes, and automation recipes.

With no topic the whole reference is printed — this is the intended way for an
AI agent to ingest what gopm can do in a single call. Pass one or more topic
names to print just those sections, or --list for the index.

Color is applied only when stdout is a terminal, so redirected or piped output
is plain text. Use "--color always" to keep the escape codes and "--color
never" to drop them; NO_COLOR is honored.`,
	Example: `  # Ingest every capability (agents: start here)
  gopm docs

  # Same reference as structured JSON
  gopm docs --json

  # A single topic, or several
  gopm docs start
  gopm docs ecosystem config

  # Index of available topics
  gopm docs --list

  # Keep colors when paging
  gopm docs --color always | less -R`,
	Args: cobra.ArbitraryArgs,
	Run:  runDocs,
}

func init() {
	f := docsCmd.Flags()
	f.StringVar(&docsColorMode, "color", "auto", "colorize output: auto|always|never")
	f.BoolVarP(&docsListOnly, "list", "l", false, "list topic names and summaries only")
}

func runDocs(cmd *cobra.Command, args []string) {
	topics, err := selectDocTopics(docTopics(), args)
	if err != nil {
		exitError(err.Error())
	}

	if jsonOutput {
		data, err := json.MarshalIndent(docsPayload{Version: Version, Topics: topics}, "", "  ")
		if err != nil {
			exitError(fmt.Sprintf("failed to encode docs: %v", err))
		}
		fmt.Println(string(data))
		return
	}

	pen, err := newDocPen(docsColorMode, os.Stdout)
	if err != nil {
		exitError(err.Error())
	}

	if docsListOnly {
		renderDocIndex(os.Stdout, pen, topics)
		return
	}
	renderDocs(os.Stdout, pen, topics, len(args) == 0)
}

// docsPayload is the --json shape: the reference plus the binary version it
// was generated from, so a cached copy can be invalidated on upgrade.
type docsPayload struct {
	Version string     `json:"version"`
	Topics  []docTopic `json:"topics"`
}

// selectDocTopics returns the topics named by args, in the order requested.
// With no args every topic is returned in reference order.
func selectDocTopics(all []docTopic, names []string) ([]docTopic, error) {
	if len(names) == 0 {
		return all, nil
	}
	index := make(map[string]docTopic, len(all))
	for _, t := range all {
		index[t.Name] = t
	}
	selected := make([]docTopic, 0, len(names))
	for _, name := range names {
		t, ok := index[strings.ToLower(name)]
		if !ok {
			return nil, fmt.Errorf("unknown docs topic %q — run 'gopm docs --list' for the index", name)
		}
		selected = append(selected, t)
	}
	return selected, nil
}

// --- rendering ---

// docPen applies ANSI styling when color is enabled and is a no-op otherwise,
// so every render path is written once regardless of the color mode.
type docPen struct{ on bool }

func newDocPen(mode string, out *os.File) (docPen, error) {
	switch mode {
	case "always":
		return docPen{on: true}, nil
	case "never":
		return docPen{on: false}, nil
	case "auto", "":
		if os.Getenv("NO_COLOR") != "" {
			return docPen{on: false}, nil
		}
		return docPen{on: isTerminal(out)}, nil
	default:
		return docPen{}, fmt.Errorf("invalid --color %q: expected auto, always, or never", mode)
	}
}

func (p docPen) style(code, s string) string {
	if !p.on || s == "" {
		return s
	}
	return code + s + display.CReset
}

func (p docPen) bold(s string) string   { return p.style(display.CBold, s) }
func (p docPen) dim(s string) string    { return p.style(display.CDim, s) }
func (p docPen) cyan(s string) string   { return p.style(display.CCyan, s) }
func (p docPen) green(s string) string  { return p.style(display.CGreen, s) }
func (p docPen) yellow(s string) string { return p.style(display.CYellow, s) }

// isTerminal reports whether f is a character device, i.e. an interactive
// terminal rather than a pipe or a file.
func isTerminal(f *os.File) bool {
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

// renderDocs writes the reference. withHeader adds the document preamble, which
// is useful for a full dump but noise when a single topic was requested.
func renderDocs(w io.Writer, p docPen, topics []docTopic, withHeader bool) {
	if withHeader {
		renderDocHeader(w, p, topics)
	}
	group := ""
	for i, t := range topics {
		if withHeader && t.Group != group {
			group = t.Group
			fmt.Fprintf(w, "\n%s\n", p.bold("# "+group))
		}
		if i > 0 || withHeader {
			fmt.Fprintln(w)
		}
		renderDocTopic(w, p, t)
	}
}

func renderDocHeader(w io.Writer, p docPen, topics []docTopic) {
	fmt.Fprintf(w, "%s\n", p.bold(fmt.Sprintf("gopm %s — complete capability reference", Version)))
	fmt.Fprintf(w, "%s\n", p.dim(fmt.Sprintf("%d topics · generated by `gopm docs` · `gopm docs --json` for machine-readable output", len(topics))))
	fmt.Fprintf(w, "%s\n", p.dim("`gopm docs <topic>` prints one section · `gopm docs --list` prints the index"))
}

func renderDocIndex(w io.Writer, p docPen, topics []docTopic) {
	fmt.Fprintf(w, "%s\n", p.bold(fmt.Sprintf("%d topics — run: gopm docs <topic>", len(topics))))

	width := 0
	for _, t := range topics {
		if len(t.Name) > width {
			width = len(t.Name)
		}
	}

	group := ""
	for _, t := range topics {
		if t.Group != group {
			group = t.Group
			fmt.Fprintf(w, "\n%s\n", p.yellow(group+":"))
		}
		fmt.Fprintf(w, "  %s  %s\n", p.cyan(padPlain(t.Name, width)), t.Summary)
	}
}

func renderDocTopic(w io.Writer, p docPen, t docTopic) {
	fmt.Fprintf(w, "%s %s %s\n", p.dim("##"), p.cyan(p.bold(t.Name)), p.dim("— "+t.Summary))

	for _, para := range t.Body {
		fmt.Fprintln(w)
		for _, line := range wrapText(para, docsWidth) {
			fmt.Fprintln(w, line)
		}
	}

	if len(t.Usage) > 0 {
		docSectionLabel(w, p, "Usage")
		for _, u := range t.Usage {
			fmt.Fprintf(w, "  %s\n", p.green(u))
		}
	}

	if len(t.Aliases) > 0 {
		docSectionLabel(w, p, "Aliases")
		fmt.Fprintf(w, "  %s\n", strings.Join(t.Aliases, ", "))
	}

	if len(t.Flags) > 0 {
		docSectionLabel(w, p, "Flags")
		renderDocFlags(w, p, t.Flags)
	}

	if len(t.Examples) > 0 {
		docSectionLabel(w, p, "Examples")
		for i, ex := range t.Examples {
			if i > 0 {
				fmt.Fprintln(w)
			}
			if ex.Desc != "" {
				fmt.Fprintf(w, "  %s\n", p.dim("# "+ex.Desc))
			}
			for _, line := range strings.Split(ex.Cmd, "\n") {
				fmt.Fprintf(w, "  %s\n", p.green(line))
			}
		}
	}

	if len(t.Notes) > 0 {
		docSectionLabel(w, p, "Notes")
		for _, note := range t.Notes {
			for _, line := range wrapIndent(note, docsWidth, "  - ", "    ") {
				fmt.Fprintln(w, line)
			}
		}
	}

	if len(t.SeeAlso) > 0 {
		docSectionLabel(w, p, "See also")
		fmt.Fprintf(w, "  %s\n", p.dim("gopm docs "+strings.Join(t.SeeAlso, " ")))
	}
}

func docSectionLabel(w io.Writer, p docPen, label string) {
	fmt.Fprintf(w, "\n%s\n", p.yellow(label+":"))
}

// renderDocFlags prints an aligned flag table. Descriptions are wrapped with a
// hanging indent under the description column; the flag specs themselves are
// never wrapped because they must stay copy-pasteable.
func renderDocFlags(w io.Writer, p docPen, flags []docFlag) {
	width := 0
	for _, f := range flags {
		if n := len(flagSpec(f)); n > width {
			width = n
		}
	}
	// 2 leading spaces + spec column + 2 separating spaces.
	descCol := width + 4
	avail := docsWidth - descCol
	if avail < docsMinDescWidth {
		avail = docsMinDescWidth
	}
	hang := strings.Repeat(" ", descCol)

	for _, f := range flags {
		spec := flagSpec(f)
		// Wrap on the plain text so the column math is not thrown off by escape
		// codes, then re-apply the dim style to the default marker afterwards.
		desc := f.Desc
		suffix := ""
		if f.Default != "" {
			suffix = "(default: " + f.Default + ")"
			desc += " " + suffix
		}
		lines := wrapText(desc, avail)

		for i, line := range lines {
			if suffix != "" && strings.Contains(line, suffix) {
				line = strings.Replace(line, suffix, p.dim(suffix), 1)
			}
			if i == 0 {
				// Pad outside the color span so the escapes stay tight around
				// the flag and alignment is computed on plain text.
				fmt.Fprintf(w, "  %s%s  %s\n", p.cyan(spec), strings.Repeat(" ", width-len(spec)), line)
				continue
			}
			fmt.Fprintf(w, "%s%s\n", hang, line)
		}
	}
}

// flagSpec renders the invocation form of a flag, e.g. "-n, --lines <n>".
// Flags without a shorthand are indented so the long forms stay aligned.
func flagSpec(f docFlag) string {
	var b strings.Builder
	if f.Short != "" {
		b.WriteString(f.Short + ", ")
	} else {
		b.WriteString("    ")
	}
	b.WriteString(f.Long)
	if f.Arg != "" {
		b.WriteString(" " + f.Arg)
	}
	return b.String()
}

// padPlain right-pads an uncolored string to width.
func padPlain(s string, width int) string {
	if len(s) >= width {
		return s
	}
	return s + strings.Repeat(" ", width-len(s))
}

// wrapText word-wraps a paragraph to width columns.
func wrapText(s string, width int) []string {
	return wrapIndent(s, width, "", "")
}

// wrapIndent word-wraps s, prefixing the first line with first and every
// subsequent line with rest. Pre-formatted lines (anything containing a
// newline) are passed through untouched so tables and JSON keep their shape.
func wrapIndent(s string, width int, first, rest string) []string {
	if strings.Contains(s, "\n") {
		lines := strings.Split(s, "\n")
		out := make([]string, 0, len(lines))
		for i, line := range lines {
			prefix := rest
			if i == 0 {
				prefix = first
			}
			out = append(out, prefix+line)
		}
		return out
	}

	words := strings.Fields(s)
	if len(words) == 0 {
		return []string{first}
	}

	var out []string
	line := first + words[0]
	for _, word := range words[1:] {
		if len(line)+1+len(word) > width {
			out = append(out, line)
			line = rest + word
			continue
		}
		line += " " + word
	}
	return append(out, line)
}

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/ampworth/cli/internal/api"
)

func cmdList(ctx context.Context, c *api.Client, opts options, w io.Writer) error {
	calcs, err := c.Calculators(ctx)
	if err != nil {
		return err
	}
	if opts.json {
		return printJSON(w, calcs)
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "CALCULATOR\tSTANDARD\tWHAT IT WORKS OUT")
	for _, calc := range calcs {
		fmt.Fprintf(tw, "%s\t%s\t%s\n", calc.ID, calc.Standard, calc.Title)
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	fmt.Fprintln(w, "\nStart with solar-pv-system-size or heat-pump-size. Run 'ampworth describe <calculator>' for its fields.")
	return nil
}

func cmdDescribe(ctx context.Context, c *api.Client, opts options, id string, w io.Writer) error {
	calc, err := c.Calculator(ctx, id)
	if err != nil {
		return err
	}
	if opts.json {
		return printJSON(w, calc)
	}
	fmt.Fprintf(w, "%s (%s)\n%s\n\n", calc.Title, calc.Standard, calc.Summary)
	if calc.InputSchema == nil {
		return nil
	}
	required := map[string]bool{}
	for _, name := range calc.InputSchema.Required {
		required[name] = true
	}
	fmt.Fprintln(w, "Fields:")
	for _, name := range fieldOrder(calc.InputSchema) {
		prop := calc.InputSchema.Properties[name]
		kind := prop.TypeName()
		if kind == "array" || kind == "object" {
			kind += ", as JSON"
		}
		marker := ""
		if required[name] {
			marker = ", required"
		}
		fmt.Fprintf(w, "  --%s  (%s%s)\n", kebab(name), kind, marker)
		fmt.Fprintf(w, "      %s\n", wrap(prop.Desc, 74, "      "))
	}
	if len(calc.Example) > 0 {
		fmt.Fprintf(w, "\nExample:\n  ampworth run %s%s\n", calc.ID, exampleFlags(calc.Example))
	}
	return nil
}

func cmdRun(ctx context.Context, c *api.Client, opts options, id string, args []string, w io.Writer) error {
	calc, err := c.Calculator(ctx, id)
	if err != nil {
		return err
	}
	fields, err := parseFields(calc, args)
	if err != nil {
		return err
	}
	res, err := c.Run(ctx, id, fields)
	if err != nil {
		return err
	}
	if opts.json {
		return printJSON(w, res)
	}
	if res.Summary == "" {
		// An older server sends no summary, so show the structured answer.
		return printJSON(w, res.Result)
	}
	fmt.Fprintln(w, res.Summary)
	if res.Notice != "" {
		fmt.Fprintf(w, "\n%s\n", wrap(res.Notice, 80, ""))
	}
	return nil
}

// parseFields reads --field value pairs into query parameters. A field can
// be written as its JSON name (--capacityKwp) or in kebab case
// (--capacity-kwp). A boolean field given with no value means true.
func parseFields(calc api.Calculator, args []string) (url.Values, error) {
	byKey := map[string]string{}
	if calc.InputSchema != nil {
		for name := range calc.InputSchema.Properties {
			byKey[normalise(name)] = name
		}
	}
	fields := url.Values{}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if !strings.HasPrefix(arg, "--") || len(arg) == 2 {
			return nil, usageError{fmt.Sprintf("expected a --field, got %q", arg)}
		}
		key, value, hasValue := strings.Cut(arg[2:], "=")
		name, ok := byKey[normalise(key)]
		if !ok {
			return nil, usageError{fmt.Sprintf("%s has no field --%s. Run 'ampworth describe %s' for its fields.", calc.ID, key, calc.ID)}
		}
		if !hasValue {
			isBool := calc.InputSchema.Properties[name].TypeName() == "boolean"
			next := i+1 < len(args) && !strings.HasPrefix(args[i+1], "--")
			switch {
			case next && (!isBool || isBoolWord(args[i+1])):
				i++
				value = args[i]
			case isBool:
				value = "true"
			default:
				return nil, usageError{fmt.Sprintf("--%s needs a value", key)}
			}
		}
		fields.Set(name, value)
	}
	return fields, nil
}

func isBoolWord(s string) bool {
	_, err := strconv.ParseBool(s)
	return err == nil
}

func cmdSearch(ctx context.Context, c *api.Client, opts options, args []string, w io.Writer) error {
	limit := 10
	var words []string
	for i := 0; i < len(args); i++ {
		if args[i] == "--limit" && i+1 < len(args) {
			n, err := strconv.Atoi(args[i+1])
			if err != nil || n < 1 {
				return usageError{"--limit takes a whole number"}
			}
			limit, i = n, i+1
			continue
		}
		words = append(words, args[i])
	}
	if len(words) == 0 {
		return usageError{"search needs some words, such as \"flow temperature\""}
	}
	docs, err := c.Search(ctx, strings.Join(words, " "), limit)
	if err != nil {
		return err
	}
	if opts.json {
		return printJSON(w, docs)
	}
	if len(docs) == 0 {
		fmt.Fprintln(w, "Nothing matched.")
		return nil
	}
	printDocs(w, docs)
	return nil
}

func cmdDocs(ctx context.Context, c *api.Client, opts options, args []string, w io.Writer) error {
	var kind, category string
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--kind" && i+1 < len(args):
			kind, i = args[i+1], i+1
		case args[i] == "--category" && i+1 < len(args):
			category, i = args[i+1], i+1
		default:
			return usageError{fmt.Sprintf("docs takes --kind and --category, not %q", args[i])}
		}
	}
	docs, err := c.Documents(ctx, kind, category)
	if err != nil {
		return err
	}
	if opts.json {
		return printJSON(w, docs)
	}
	printDocs(w, docs)
	return nil
}

func cmdRead(ctx context.Context, c *api.Client, opts options, ref string, w io.Writer) error {
	// Accept guides/slug, guide/slug, or a full ampworth.com address.
	if u, err := url.Parse(ref); err == nil && u.Host != "" {
		ref = strings.TrimSuffix(strings.Trim(u.Path, "/"), ".md")
	}
	kind, slug, ok := strings.Cut(strings.Trim(ref, "/"), "/")
	if !ok || slug == "" {
		return usageError{"read takes kind/slug, such as guides/sizing-a-heat-pump"}
	}
	doc, err := c.Document(ctx, kind, slug)
	if err != nil {
		return err
	}
	if opts.json {
		return printJSON(w, doc)
	}
	fmt.Fprintf(w, "# %s\n\n%s\n\n%s\n\nSource: %s\n", doc.Title, doc.Summary, strings.TrimSpace(doc.Text), doc.URL)
	return nil
}

func printDocs(w io.Writer, docs []api.Document) {
	for i, d := range docs {
		if i > 0 {
			fmt.Fprintln(w)
		}
		fmt.Fprintf(w, "%s  (%s, %d min)\n", d.Title, d.Kind, d.ReadingMinutes)
		fmt.Fprintf(w, "  ampworth read %ss/%s\n", d.Kind, d.Slug)
		fmt.Fprintf(w, "  %s\n", wrap(d.Summary, 78, "  "))
	}
}

func printJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// fieldOrder lists a schema's fields required first, then alphabetically.
func fieldOrder(s *api.Schema) []string {
	required := map[string]bool{}
	for _, name := range s.Required {
		required[name] = true
	}
	names := make([]string, 0, len(s.Properties))
	for name := range s.Properties {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool {
		if required[names[i]] != required[names[j]] {
			return required[names[i]]
		}
		return names[i] < names[j]
	})
	return names
}

func exampleFlags(example map[string]any) string {
	names := make([]string, 0, len(example))
	for name := range example {
		names = append(names, name)
	}
	sort.Strings(names)
	var b strings.Builder
	for _, name := range names {
		var value string
		switch v := example[name].(type) {
		case string:
			value = v
		case bool:
			if v {
				fmt.Fprintf(&b, " --%s", kebab(name))
				continue
			}
			value = "false"
		default:
			raw, _ := json.Marshal(v)
			value = string(raw)
		}
		if strings.ContainsAny(value, " \"'{}[]") {
			value = "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
		}
		fmt.Fprintf(&b, " --%s %s", kebab(name), value)
	}
	return b.String()
}

// kebab turns a JSON field name into a flag: capacityKwp becomes capacity-kwp.
func kebab(name string) string {
	var b strings.Builder
	for i, r := range name {
		if r >= 'A' && r <= 'Z' {
			if i > 0 {
				b.WriteByte('-')
			}
			r += 'a' - 'A'
		}
		b.WriteRune(r)
	}
	return b.String()
}

// normalise compares field names regardless of case and separators.
func normalise(name string) string {
	return strings.NewReplacer("-", "", "_", "").Replace(strings.ToLower(name))
}

func wrap(text string, width int, indent string) string {
	words := strings.Fields(text)
	var b strings.Builder
	line := 0
	for _, word := range words {
		if line > 0 && line+1+len(word) > width {
			b.WriteString("\n" + indent)
			line = 0
		} else if line > 0 {
			b.WriteByte(' ')
			line++
		}
		b.WriteString(word)
		line += len(word)
	}
	return b.String()
}

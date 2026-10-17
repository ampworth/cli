package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	"github.com/ampworth/cli/internal/api"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// instructions are what the local server tells a client when it connects.
// They follow the hosted server's, so an assistant behaves the same either way.
const instructions = `Ampworth covers homes in the United Kingdom only. Its methods come from the published MCS standards, and the network connection route from the ENA's G98 and G99. Don't apply them to another country: say the tools don't cover it.

Every answer is for understanding a system or checking a quotation, not a design. Say that an installation still needs a survey and a design from a certified installer. Ampworth recommends no installer; to find one, point to https://mcscertified.com/find-an-installer/.

Ask for a required input rather than guessing it, especially a postcode, a roof direction or a year of energy use. When an answer relies on a default, say which one. Pass on the figures, the standard and the warnings each result returns.

Calculators:
- When someone asks what system they need, start with solar_pv_system_size or heat_pump_size. Use the single-method tools to check a figure they already have.
- To check a quoted heat pump, give its output to heat_pump_size as quotedHeatPumpKw.
- When a field must be one of a fixed set, such as a design location, room type or fuel, call list_calculators for the values.
- Prices are in pounds per unit (0.27, not 27p), and efficiencies and fractions are decimals (0.85, not 85%).

Documents:
- For an explanation, call search_documents, then get_document for the best match, and answer from it. Link to the page's url.

When a question needs both, calculate first, then use the documents to explain the result and what to ask an installer.`

// serveMCP runs a local MCP server over stdin and stdout. Its tools are built
// from the API at start-up, so a calculator added to the site appears here
// without a new release.
func serveMCP(ctx context.Context, c *api.Client, stdin io.Reader, stdout io.Writer) error {
	calcs, err := loadCalculators(ctx, c)
	if err != nil {
		return fmt.Errorf("could not load the calculators: %w", err)
	}

	server := mcp.NewServer(&mcp.Implementation{
		Name:       "ampworth",
		Title:      "Ampworth: UK home energy calculators and guides",
		Version:    version,
		WebsiteURL: "https://ampworth.com/developers",
	}, &mcp.ServerOptions{Instructions: instructions})

	for _, calc := range calcs {
		addCalculatorTool(server, c, calc)
	}
	addListCalculators(server, calcs)
	addDocumentTools(server, c)

	var transport mcp.Transport = &mcp.IOTransport{Reader: io.NopCloser(stdin), Writer: nopWriteCloser{stdout}}
	if stdin == os.Stdin && stdout == os.Stdout {
		transport = &mcp.StdioTransport{}
	}
	err = server.Run(ctx, transport)
	// A client ends a stdio session by closing the server's input, so that,
	// or an interrupt, is a normal exit rather than an error.
	if err == nil || errors.Is(err, io.EOF) || errors.Is(err, context.Canceled) || strings.HasSuffix(err.Error(), "EOF") {
		return nil
	}
	return err
}

type nopWriteCloser struct{ io.Writer }

func (nopWriteCloser) Close() error { return nil }

// loadCalculators fetches every calculator with its schemas, in parallel.
func loadCalculators(ctx context.Context, c *api.Client) ([]api.Calculator, error) {
	list, err := c.Calculators(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]api.Calculator, len(list))
	errs := make([]error, len(list))
	var wg sync.WaitGroup
	for i, calc := range list {
		wg.Add(1)
		go func() {
			defer wg.Done()
			out[i], errs[i] = c.Calculator(ctx, calc.ID)
		}()
	}
	wg.Wait()
	return out, errors.Join(errs...)
}

func annotations(title string) *mcp.ToolAnnotations {
	f := false
	return &mcp.ToolAnnotations{Title: title, ReadOnlyHint: true, DestructiveHint: &f, IdempotentHint: true, OpenWorldHint: &f}
}

func addCalculatorTool(server *mcp.Server, c *api.Client, calc api.Calculator) {
	tool := &mcp.Tool{
		Name:  calc.ToolName(),
		Title: calc.Title,
		Description: strings.TrimSpace(calc.Description + "\n\nMethod from " + calc.Standard +
			". These calculators follow the published MCS methods, for understanding a system and checking a quotation. " +
			"Anything that gets installed still needs a design and a site survey from a certified installer."),
		InputSchema: json.RawMessage(calc.InputSchema.Raw),
		Annotations: annotations(calc.Title),
	}
	if len(calc.OutputSchema) > 0 {
		tool.OutputSchema = calc.OutputSchema
	}
	server.AddTool(tool, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		res, err := c.RunJSON(ctx, calc.ID, req.Params.Arguments)
		if err != nil {
			// A tool error, so the model reads what it got wrong and can
			// correct the call itself.
			var out mcp.CallToolResult
			out.SetError(err)
			return &out, nil
		}
		text := res.Summary
		if text == "" {
			text = string(res.Result)
		}
		return &mcp.CallToolResult{
			Content:           []mcp.Content{&mcp.TextContent{Text: text}},
			StructuredContent: res.Result,
		}, nil
	})
}

type calculatorEntry struct {
	ID       string `json:"id"`
	Tool     string `json:"tool"`
	Title    string `json:"title"`
	Summary  string `json:"summary"`
	Standard string `json:"standard"`
	Tier     string `json:"tier"`
}

func addListCalculators(server *mcp.Server, calcs []api.Calculator) {
	title := "List the calculators and their reference data"
	mcp.AddTool(server, &mcp.Tool{
		Name:  "list_calculators",
		Title: title,
		Description: "Lists every calculator, with the standard behind it and whether it is a whole-system sizing calculator or a " +
			"single-method one, plus the reference data a caller needs to build a valid request, such as the solar postcode " +
			"zones, the design locations, the room types and the fuels. Call this when a value must be one of a fixed set.",
		Annotations: annotations(title),
	}, func(context.Context, *mcp.CallToolRequest, struct{}) (*mcp.CallToolResult, map[string]any, error) {
		entries := make([]calculatorEntry, 0, len(calcs))
		reference := map[string]any{}
		for _, calc := range calcs {
			entries = append(entries, calculatorEntry{calc.ID, calc.ToolName(), calc.Title, calc.Summary, calc.Standard, calc.Tier})
			for k, v := range calc.Reference {
				reference[k] = v
			}
		}
		return nil, map[string]any{"calculators": entries, "reference": reference}, nil
	})
}

type searchArgs struct {
	Query string `json:"query" jsonschema:"What to search for, in plain words."`
	Limit int    `json:"limit,omitempty" jsonschema:"How many results to return, from 1 to 50. Defaults to 10."`
}

type getArgs struct {
	Kind string `json:"kind" jsonschema:"The document's kind: 'guide', 'article' or 'faq'."`
	Slug string `json:"slug" jsonschema:"The document's slug, as returned by search_documents or list_documents."`
}

type listArgs struct {
	Kind     string `json:"kind,omitempty" jsonschema:"Only this kind: 'guide', 'article' or 'faq'."`
	Category string `json:"category,omitempty" jsonschema:"Only this subject, such as 'heat-pumps' or 'solar-pv'."`
}

func addDocumentTools(server *mcp.Server, c *api.Client) {
	searchTitle := "Search the documents"
	mcp.AddTool(server, &mcp.Tool{
		Name:  "search_documents",
		Title: searchTitle,
		Description: "Searches Ampworth's guides, articles and question sets on UK heat pumps, solar PV, batteries, MCS " +
			"certification and the paperwork around an installation, and ranks the results. Then call get_document for the full text.",
		Annotations: annotations(searchTitle),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in searchArgs) (*mcp.CallToolResult, map[string]any, error) {
		docs, err := c.Search(ctx, in.Query, in.Limit)
		if err != nil {
			return nil, nil, err
		}
		return nil, map[string]any{"results": docs}, nil
	})

	getTitle := "Read one document in full"
	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_document",
		Title:       getTitle,
		Description: "Returns one guide, article or question set in full, as Markdown, with its address to cite.",
		Annotations: annotations(getTitle),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in getArgs) (*mcp.CallToolResult, api.Document, error) {
		doc, err := c.Document(ctx, in.Kind, in.Slug)
		return nil, doc, err
	})

	listTitle := "List what the documents cover"
	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_documents",
		Title:       listTitle,
		Description: "Lists the guides, articles and question sets, optionally filtered by kind or subject, without their full text.",
		Annotations: annotations(listTitle),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in listArgs) (*mcp.CallToolResult, map[string]any, error) {
		docs, err := c.Documents(ctx, in.Kind, in.Category)
		if err != nil {
			return nil, nil, err
		}
		return nil, map[string]any{"documents": docs}, nil
	})
}

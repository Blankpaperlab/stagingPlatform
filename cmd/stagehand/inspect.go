package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	"stagehand/internal/config"
	"stagehand/internal/recorder"
	"stagehand/internal/store"
	sqlitestore "stagehand/internal/store/sqlite"
)

type inspectNode struct {
	interaction recorder.Interaction
	children    []*inspectNode
}

func runInspect(args []string, stdout io.Writer, _ io.Writer) error {
	flags := flag.NewFlagSet("inspect", flag.ContinueOnError)
	flags.SetOutput(io.Discard)

	runID := flags.String("run-id", "", "Run identifier to inspect")
	session := flags.String("session", "", "Session name to inspect latest run from")
	configPath := flags.String("config", "", "Path to stagehand.yml")
	showBodies := flags.Bool("show-bodies", false, "Expand request bodies and event payloads")
	if err := flags.Parse(args); err != nil {
		return fmt.Errorf("parse inspect flags: %w\n\n%s", err, inspectHelpText())
	}

	resolvedRunID := strings.TrimSpace(*runID)
	resolvedSession := strings.TrimSpace(*session)
	if (resolvedRunID == "" && resolvedSession == "") || (resolvedRunID != "" && resolvedSession != "") {
		return fmt.Errorf("inspect requires exactly one of --run-id or --session\n\n%s", inspectHelpText())
	}

	cfgPath := strings.TrimSpace(*configPath)
	if cfgPath == "" {
		cfgPath = defaultRuntimeConfigPath
	}

	ctx := context.Background()
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return fmt.Errorf("load runtime config %q: %w", cfgPath, err)
	}

	dbPath := sqliteDatabasePath(cfg.Record.StoragePath)
	sqliteStore, err := sqlitestore.OpenStore(ctx, dbPath)
	if err != nil {
		return fmt.Errorf("open local store %q: %w", dbPath, err)
	}
	defer sqliteStore.Close()

	runRecord, interactions, err := loadInspectableRun(ctx, sqliteStore, resolvedRunID, resolvedSession)
	if err != nil {
		return err
	}

	return renderInspect(stdout, runRecord, interactions, *showBodies)
}

func loadInspectableRun(
	ctx context.Context,
	artifactStore store.ArtifactStore,
	runID string,
	session string,
) (store.RunRecord, []recorder.Interaction, error) {
	var (
		runRecord store.RunRecord
		err       error
	)

	if strings.TrimSpace(runID) != "" {
		runRecord, err = artifactStore.GetRunRecord(ctx, runID)
		if err != nil {
			return store.RunRecord{}, nil, fmt.Errorf("load run record %q: %w", runID, err)
		}
	} else {
		runRecord, err = artifactStore.GetLatestRunRecord(ctx, session)
		if err != nil {
			return store.RunRecord{}, nil, fmt.Errorf("load latest run record for session %q: %w", session, err)
		}
	}

	interactions, err := artifactStore.ListInteractions(ctx, runRecord.RunID)
	if err != nil {
		return store.RunRecord{}, nil, fmt.Errorf("list interactions for run %q: %w", runRecord.RunID, err)
	}

	return runRecord, interactions, nil
}

func renderInspect(
	w io.Writer,
	runRecord store.RunRecord,
	interactions []recorder.Interaction,
	showBodies bool,
) error {
	if _, err := fmt.Fprintf(w, "Run: %s\n", runRecord.RunID); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "Session: %s\n", runRecord.SessionName); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "Mode: %s\n", runRecord.Mode); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "Status: %s\n", strings.ToUpper(string(runRecord.Status))); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "Started: %s\n", runRecord.StartedAt.Format(time.RFC3339)); err != nil {
		return err
	}

	endedAt := "<running>"
	if runRecord.EndedAt != nil {
		endedAt = runRecord.EndedAt.Format(time.RFC3339)
	}
	if _, err := fmt.Fprintf(w, "Ended: %s\n", endedAt); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "Interactions: %d\n", len(interactions)); err != nil {
		return err
	}

	if len(runRecord.IntegrityIssues) > 0 {
		if _, err := io.WriteString(w, "\nIntegrity Issues:\n"); err != nil {
			return err
		}
		for _, issue := range runRecord.IntegrityIssues {
			if _, err := fmt.Fprintf(w, "- %s: %s\n", issue.Code, issue.Message); err != nil {
				return err
			}
		}
	}

	if injectionMetadata, ok := runRecord.Metadata["error_injection"]; ok {
		if _, err := io.WriteString(w, "\nError Injection:\n"); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(w, "%s\n", indentBlock(prettyJSON(injectionMetadata), "  ")); err != nil {
			return err
		}
	}

	if _, err := io.WriteString(w, "\nInteractions:\n"); err != nil {
		return err
	}
	if len(interactions) == 0 {
		_, err := io.WriteString(w, "(none)\n")
		return err
	}

	roots := buildInspectTree(interactions)
	visited := map[string]bool{}
	for _, root := range roots {
		if err := renderInspectNode(w, root, 0, showBodies, visited); err != nil {
			return err
		}
	}

	return nil
}

func buildInspectTree(interactions []recorder.Interaction) []*inspectNode {
	nodes := make(map[string]*inspectNode, len(interactions))
	for _, interaction := range interactions {
		copied := interaction
		nodes[interaction.InteractionID] = &inspectNode{interaction: copied}
	}

	roots := make([]*inspectNode, 0, len(interactions))
	for _, interaction := range interactions {
		node := nodes[interaction.InteractionID]
		parentID := strings.TrimSpace(interaction.ParentInteractionID)
		if parentID == "" {
			roots = append(roots, node)
			continue
		}

		parent, ok := nodes[parentID]
		if !ok {
			roots = append(roots, node)
			continue
		}
		parent.children = append(parent.children, node)
	}

	for _, node := range nodes {
		slices.SortFunc(node.children, func(a, b *inspectNode) int {
			if a.interaction.Sequence == b.interaction.Sequence {
				return strings.Compare(a.interaction.InteractionID, b.interaction.InteractionID)
			}
			return a.interaction.Sequence - b.interaction.Sequence
		})
	}
	slices.SortFunc(roots, func(a, b *inspectNode) int {
		if a.interaction.Sequence == b.interaction.Sequence {
			return strings.Compare(a.interaction.InteractionID, b.interaction.InteractionID)
		}
		return a.interaction.Sequence - b.interaction.Sequence
	})

	return roots
}

func renderInspectNode(
	w io.Writer,
	node *inspectNode,
	depth int,
	showBodies bool,
	visited map[string]bool,
) error {
	if visited[node.interaction.InteractionID] {
		_, err := fmt.Fprintf(
			w,
			"%s- [%d] %s %s (cycle detected for interaction %s)\n",
			strings.Repeat("  ", depth),
			node.interaction.Sequence,
			emptyFallback(node.interaction.Service, "<unknown-service>"),
			emptyFallback(node.interaction.Operation, "<unknown-operation>"),
			node.interaction.InteractionID,
		)
		return err
	}
	visited[node.interaction.InteractionID] = true
	defer delete(visited, node.interaction.InteractionID)

	if _, err := fmt.Fprintf(
		w,
		"%s- [%d] %s %s protocol=%s latency=%s fallback=%s\n",
		strings.Repeat("  ", depth),
		node.interaction.Sequence,
		emptyFallback(node.interaction.Service, "<unknown-service>"),
		emptyFallback(node.interaction.Operation, "<unknown-operation>"),
		emptyFallback(string(node.interaction.Protocol), "unknown"),
		formatLatency(node.interaction.LatencyMS),
		formatFallback(node.interaction.FallbackTier),
	); err != nil {
		return err
	}

	if _, err := fmt.Fprintf(
		w,
		"%s  request: %s %s\n",
		strings.Repeat("  ", depth),
		emptyFallback(node.interaction.Request.Method, "<unknown-method>"),
		emptyFallback(node.interaction.Request.URL, "<unknown-url>"),
	); err != nil {
		return err
	}

	if showBodies && node.interaction.Request.Body != nil {
		if _, err := fmt.Fprintf(
			w,
			"%s  request body:\n%s\n",
			strings.Repeat("  ", depth),
			indentBlock(prettyJSON(node.interaction.Request.Body), strings.Repeat("  ", depth)+"    "),
		); err != nil {
			return err
		}
	}

	if _, err := fmt.Fprintf(w, "%s  events:\n", strings.Repeat("  ", depth)); err != nil {
		return err
	}
	for _, event := range node.interaction.Events {
		nested := ""
		if event.NestedInteractionID != "" {
			nested = fmt.Sprintf(" nested=%s", event.NestedInteractionID)
		}
		if _, err := fmt.Fprintf(
			w,
			"%s    - #%d %s t=%dms sim=%dms%s\n",
			strings.Repeat("  ", depth),
			event.Sequence,
			event.Type,
			event.TMS,
			event.SimTMS,
			nested,
		); err != nil {
			return err
		}

		if showBodies && event.Data != nil {
			if _, err := fmt.Fprintf(
				w,
				"%s      data:\n%s\n",
				strings.Repeat("  ", depth),
				indentBlock(prettyJSON(event.Data), strings.Repeat("  ", depth)+"        "),
			); err != nil {
				return err
			}
		}
	}

	for _, child := range node.children {
		if err := renderInspectNode(w, child, depth+1, showBodies, visited); err != nil {
			return err
		}
	}

	return nil
}

func prettyJSON(value any) string {
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Sprintf("%v", value)
	}
	return string(encoded)
}

func indentBlock(value string, prefix string) string {
	lines := strings.Split(value, "\n")
	for idx, line := range lines {
		lines[idx] = prefix + line
	}
	return strings.Join(lines, "\n")
}

func formatLatency(latencyMS int64) string {
	if latencyMS <= 0 {
		return "n/a"
	}
	return fmt.Sprintf("%dms", latencyMS)
}

func formatFallback(tier recorder.FallbackTier) string {
	if tier == "" {
		return "none"
	}
	return string(tier)
}

func emptyFallback(value string, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

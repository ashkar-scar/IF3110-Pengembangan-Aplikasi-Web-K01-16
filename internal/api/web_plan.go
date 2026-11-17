package api

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/sqlguard"
	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/targetdb"
)

// PostgreSQL menghasilkan pohon (Nodes), SQLite menghasilkan daftar langkah (Steps).
type planView struct {
	Driver string
	Nodes  []planNode
	Steps  []targetdb.PlanStep
}

type planNode struct {
	Title     string
	Estimate  string
	Condition string
	Children  []planNode
}

// Jalankan EXPLAIN untuk satu statement SQL tanpa mengeksekusinya.
func (a *API) explain(ctx context.Context, target Target, sql string) (planView, error) {
	_, err := sqlguard.Plan(sql)
	if err != nil {
		return planView{}, err
	}

	planCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	conn, err := a.open(planCtx, target)
	if err != nil {
		log.Printf("Database target tidak dapat diakses: %v", err)
		return planView{}, errors.New("Database target tidak dapat diakses.")
	}
	defer conn.Close()

	plan, err := conn.Explain(planCtx, sql)
	if err != nil {
		return planView{}, errors.New("Query tidak dapat direncanakan. Periksa SQL dan nama tabel yang dipakai.")
	}

	view := planView{Driver: plan.Driver}
	switch raw := plan.Raw.(type) {
	case []targetdb.PlanStep:
		view.Steps = raw
	case []any:
		for _, item := range raw {
			if root, ok := item.(map[string]any); ok {
				view.Nodes = append(view.Nodes, newPlanNode(root["Plan"]))
			}
		}
	}

	return view, nil
}

// Susun node pohon execution plan PostgreSQL secara rekursif.
func newPlanNode(value any) planNode {
	data, _ := value.(map[string]any)
	text := func(key string) string {
		if v, ok := data[key]; ok {
			return fmt.Sprint(v)
		}
		return ""
	}

	titleParts := []string{text("Node Type")}
	if operation := text("Operation"); operation != "" {
		titleParts = append(titleParts, operation)
	}
	if join := text("Join Type"); join != "" {
		titleParts = append(titleParts, join+" join")
	}
	if relation := text("Relation Name"); relation != "" {
		titleParts = append(titleParts, "on "+relation)
	}
	if index := text("Index Name"); index != "" {
		titleParts = append(titleParts, "using "+index)
	}

	node := planNode{
		Title:    strings.Join(titleParts, " · "),
		Estimate: fmt.Sprintf("cost %s..%s · rows %s", text("Startup Cost"), text("Total Cost"), text("Plan Rows")),
	}
	for _, key := range []string{"Index Cond", "Hash Cond", "Merge Cond", "Filter"} {
		if condition := text(key); condition != "" {
			node.Condition = condition
			break
		}
	}

	children, _ := data["Plans"].([]any)
	for _, child := range children {
		node.Children = append(node.Children, newPlanNode(child))
	}

	return node
}

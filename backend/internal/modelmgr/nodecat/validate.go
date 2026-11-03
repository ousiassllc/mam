package nodecat

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/localcfg"
	"github.com/ousiassllc/mam/backend/internal/modelmgr/catalog"
)

var (
	repoPattern   = regexp.MustCompile(`^https://github\.com/([A-Za-z0-9._-]+)/([A-Za-z0-9._-]+)$`)
	commitPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)
	sha256Pattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
	// folderPattern is a plain directory name: it is given to --whitelist-custom-nodes and joined to custom_nodes, so no separator, no
	// leading dot, and not the ".disabled" suffix with which ComfyUI skips a folder.
	folderPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
)

// ArchiveRoot is the single directory the GitHub archive of the repository at Commit holds: <repo>-<commit>.
func (n Node) ArchiveRoot() string {
	m := repoPattern.FindStringSubmatch(n.Repo)
	if m == nil {
		return ""
	}
	return m[2] + "-" + n.Commit
}

// ClassesOf is the node classes the nodes with the IDs add (an ID that is not in the allowlist adds none).
func ClassesOf(c *Catalog, ids []string) []string {
	var out []string
	for _, id := range ids {
		if n, ok := c.Node(id); ok {
			out = append(out, n.NodeClasses...)
		}
	}
	return out
}

// Validate checks the allowlist: every pin, the license, the dependencies, the review, and that it agrees with the model catalog and the
// templates (a template names the custom nodes whose classes it uses, and only an installable node). engine is the pinned ComfyUI release
// (the Python package list is read from that release). It returns every problem found; nil means the allowlist can be used.
func Validate(c *Catalog, models *catalog.Catalog, template func(id string) (localcfg.Template, bool)) []error {
	var errs []error
	if c.Python.ComfyUI != models.Engine.Version || strings.TrimSpace(c.Python.Source) == "" {
		errs = append(errs, fmt.Errorf("python.json: the package list has to be read from the pinned engine %s and say where from", models.Engine.Version))
	}
	for _, p := range c.Python.Packages {
		if p == "" || p != strings.ToLower(p) {
			errs = append(errs, fmt.Errorf("python.json: package %q must be a lower-case import name", p))
		}
	}
	seen, folders := map[string]bool{}, map[string]bool{}
	for _, n := range c.Nodes {
		errs = append(errs, validateNode(c, n)...)
		if seen[n.ID] || folders[strings.ToLower(n.Folder)] {
			errs = append(errs, fmt.Errorf("node %s: the id and the folder must be used once", n.ID))
		}
		seen[n.ID], folders[strings.ToLower(n.Folder)] = true, true
	}
	return append(errs, validateUses(c, models, template)...)
}

func validateNode(c *Catalog, n Node) []error {
	where := "node " + n.ID
	var errs []error
	fail := func(format string, args ...any) {
		errs = append(errs, fmt.Errorf("%s: %s", where, fmt.Sprintf(format, args...)))
	}
	if !localcfg.ValidName(n.ID) || n.Name == "" || strings.TrimSpace(n.Description) == "" {
		fail("id (slug), name and description are required")
	}
	switch {
	case !repoPattern.MatchString(n.Repo):
		fail("repo must be https://github.com/<owner>/<repo>")
	case !commitPattern.MatchString(n.Commit):
		fail("commit must be 40 lower-case hex digits (never a branch or a tag)")
	case n.ArchiveURL != n.Repo+"/archive/"+n.Commit+".zip":
		fail("archive_url must be the archive of the repo at the pinned commit")
	}
	if !sha256Pattern.MatchString(n.ArchiveSHA256) || n.ArchiveSize <= 0 || n.ExtractedSize <= 0 {
		fail("archive_sha256 (64 lower-case hex digits), archive_size and extracted_size are required")
	}
	if !folderPattern.MatchString(n.Folder) || strings.HasSuffix(strings.ToLower(n.Folder), ".disabled") || n.Folder == "__pycache__" {
		fail("folder %q must be a plain directory name", n.Folder)
	}
	errs = append(errs, validateLicense(where, n)...)
	if len(n.PythonRequires) == 0 {
		fail("python_requires must list what the node imports (even if only torch)")
	}
	var missing []string
	for _, p := range n.PythonRequires {
		if p != strings.ToLower(p) {
			fail("python_requires %q must be a lower-case import name", p)
		}
		if !c.Python.Has(p) {
			missing = append(missing, p)
		}
	}
	switch {
	case n.Installable() && len(missing) > 0:
		fail("needs %v, which the bundled Python environment does not have: set install_blocked (mam runs no pip)", missing)
	case !n.Installable() && strings.TrimSpace(n.InstallBlocked) == "":
		fail("install_blocked must say why")
	}
	if len(n.NodeClasses) == 0 || len(slices.Compact(slices.Sorted(slices.Values(n.NodeClasses)))) != len(n.NodeClasses) {
		fail("node_classes must list the classes of the node once each")
	}
	if _, err := time.Parse(time.DateOnly, n.ReviewedAt); err != nil || strings.TrimSpace(n.Review) == "" {
		fail("reviewed_at (YYYY-MM-DD) and review (what the source does) are required")
	}
	return errs
}

// validateLicense checks the license of a node's code: the same fields as a model's, and the license text has to be the one at the pinned
// commit of the repository (not whatever the default branch says today).
func validateLicense(where string, n Node) []error {
	l := n.License
	var errs []error
	if l.ID == "" || l.Version == "" || l.Name == "" || strings.TrimSpace(l.Summary) == "" {
		errs = append(errs, fmt.Errorf("%s: the license needs id, version, name and summary", where))
	}
	if err := l.Info().Check(); err != nil {
		errs = append(errs, fmt.Errorf("%s: license: %w", where, err))
	}
	if !strings.HasPrefix(l.URL, n.Repo+"/blob/"+n.Commit+"/") {
		errs = append(errs, fmt.Errorf("%s: the license url must be the license file at the pinned commit", where))
	}
	return errs
}

// validateUses checks the templates and the model files against the nodes: a template that uses the classes of a node names it, names only
// nodes it uses, and never one that cannot be installed; a model with a file in a folder a node adds has a workflow that requires the node.
func validateUses(c *Catalog, models *catalog.Catalog, template func(id string) (localcfg.Template, bool)) []error {
	var errs []error
	for _, m := range models.Models {
		needs := map[string]bool{}
		for _, w := range m.Workflows {
			t, ok := template(w.Template)
			if !ok || t.Model.Workflow == nil {
				continue
			}
			where := fmt.Sprintf("model %s workflow %s", m.ID, w.Name)
			errs = append(errs, validateTemplateNodes(c, where, *t.Model.Workflow)...)
			for _, id := range t.Model.Workflow.RequiresNodes {
				needs[id] = true
			}
		}
		for _, f := range m.Files {
			for _, n := range c.Nodes {
				if slices.Contains(n.ModelFolders, f.Folder) && !needs[n.ID] {
					errs = append(errs, fmt.Errorf("model %s: the file %s/%s is in a folder the node %s adds, and no workflow of the model requires that node", m.ID, f.Folder, f.Name, n.ID))
				}
			}
		}
	}
	return errs
}

func validateTemplateNodes(c *Catalog, where string, w localcfg.Workflow) []error {
	var errs []error
	g, err := localcfg.ParseGraph(w.Graph)
	if err != nil {
		return nil // the catalog's own validation reports an unreadable graph
	}
	used := map[string]bool{}
	for _, n := range g {
		used[n.ClassType] = true
	}
	for _, id := range w.RequiresNodes {
		n, ok := c.Node(id)
		switch {
		case !ok:
			errs = append(errs, fmt.Errorf("%s: requires_nodes names %q, which is not in the allowlist", where, id))
		case !n.Installable():
			errs = append(errs, fmt.Errorf("%s: requires the node %s, which mam cannot install (%s)", where, id, n.InstallBlocked))
		case !slices.ContainsFunc(n.NodeClasses, func(class string) bool { return used[class] }):
			errs = append(errs, fmt.Errorf("%s: requires the node %s but the graph uses none of its classes", where, id))
		}
	}
	for _, n := range c.Nodes {
		if slices.ContainsFunc(n.NodeClasses, func(class string) bool { return used[class] }) && !slices.Contains(w.RequiresNodes, n.ID) {
			errs = append(errs, fmt.Errorf("%s: the graph uses classes of the node %s, which requires_nodes does not name", where, n.ID))
		}
	}
	return errs
}

// Package servermodel provides the internal variable/domain registry
// used by the MMS server. It stores domains, named variables, and
// (optionally) named variable lists, and supports deterministic
// iteration for GetNameList pagination.
package servermodel

import (
	"fmt"
	"sort"
	"sync"
)

// VarEntry is a registered variable in the server model.
type VarEntry struct {
	Domain    string
	ItemID    string
	Scope     int // 0=VMD, 1=Domain, 2=Association
	TypeSpec  any
	Deletable bool
	ReadFunc  any
	WriteFunc any
}

// NVLEntry is a registered named variable list in the server model.
type NVLEntry struct {
	Domain    string
	ItemID    string
	Scope     int // 0=VMD, 1=Domain
	Deletable bool
	Variables []NVLVariable
}

// AccessSelectorModel is the server-side representation of one level
// of alternate access selection inside an NVL definition.
type AccessSelectorModel struct {
	Component  string
	HasIndex   bool
	Index      int
	IndexRange *IndexRangeModel
}

// IndexRangeModel holds an index range for alternate access.
type IndexRangeModel struct {
	LowIndex         int
	NumberOfElements int
}

// NVLVariable describes one variable reference inside a named variable list,
// optionally with alternate access selectors for component/index/range scoping.
type NVLVariable struct {
	Scope           int
	DomainID        string
	ItemID          string
	AlternateAccess []AccessSelectorModel
}

// Registry holds the server's MMS object model.
type Registry struct {
	mu      sync.RWMutex
	domains map[string]bool
	vars    map[string]*VarEntry // key = scope:domain/itemID
	nvls    map[string]*NVLEntry // key = scope:domain/itemID

	domainOrder []string            // sorted domain names
	varOrder    map[string][]string // domain -> sorted variable names
	vmdVarOrder []string            // sorted VMD-scope variable names
	nvlOrder    map[string][]string // domain -> sorted NVL names
	vmdNVLOrder []string            // sorted VMD-scope NVL names
}

// NewRegistry creates an empty server model registry.
func NewRegistry() *Registry {
	return &Registry{
		domains:  make(map[string]bool),
		vars:     make(map[string]*VarEntry),
		nvls:     make(map[string]*NVLEntry),
		varOrder: make(map[string][]string),
		nvlOrder: make(map[string][]string),
	}
}

// RegisterDomain adds a domain to the registry. Returns an error if
// the domain is already registered.
func (r *Registry) RegisterDomain(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if name == "" {
		return fmt.Errorf("servermodel: empty domain name")
	}
	if r.domains[name] {
		return fmt.Errorf("servermodel: domain %q already registered", name)
	}
	r.domains[name] = true
	r.domainOrder = append(r.domainOrder, name)
	sort.Strings(r.domainOrder)
	return nil
}

// HasDomain reports whether a domain is registered.
func (r *Registry) HasDomain(name string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.domains[name]
}

func varKey(scope int, domain, itemID string) string {
	return fmt.Sprintf("%d:%s/%s", scope, domain, itemID)
}

// RegisterVariable adds a named variable. The variable's domain (if
// domain-scoped) must already be registered. Association-scope
// variables (Scope=2) should be registered on the per-connection
// ServerConn, not here.
func (r *Registry) RegisterVariable(entry *VarEntry) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if entry.ItemID == "" {
		return fmt.Errorf("servermodel: empty variable ItemID")
	}
	if entry.Scope == 1 && entry.Domain == "" {
		return fmt.Errorf("servermodel: domain-scoped variable requires non-empty domain")
	}
	if entry.Scope == 1 && !r.domains[entry.Domain] {
		return fmt.Errorf("servermodel: domain %q not registered", entry.Domain)
	}

	key := varKey(entry.Scope, entry.Domain, entry.ItemID)
	if _, exists := r.vars[key]; exists {
		return fmt.Errorf("servermodel: variable %q already registered", key)
	}
	r.vars[key] = entry

	switch entry.Scope {
	case 0: // VMD
		r.vmdVarOrder = append(r.vmdVarOrder, entry.ItemID)
		sort.Strings(r.vmdVarOrder)
	case 1: // Domain
		r.varOrder[entry.Domain] = append(r.varOrder[entry.Domain], entry.ItemID)
		sort.Strings(r.varOrder[entry.Domain])
	}
	return nil
}

// LookupVariable returns the variable entry for the given scope/domain/itemID.
func (r *Registry) LookupVariable(scope int, domain, itemID string) (*VarEntry, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	v, ok := r.vars[varKey(scope, domain, itemID)]
	return v, ok
}

// NameListResult holds a page of names for GetNameList.
type NameListResult struct {
	Names       []string
	MoreFollows bool
}

const defaultPageSize = 100

// ListDomains returns domain names starting after continueAfter.
func (r *Registry) ListDomains(continueAfter string, maxNames int) NameListResult {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return paginate(r.domainOrder, continueAfter, maxNames)
}

// ListDomainVariables returns variable names in a domain, starting after continueAfter.
func (r *Registry) ListDomainVariables(domain, continueAfter string, maxNames int) NameListResult {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := r.varOrder[domain]
	return paginate(names, continueAfter, maxNames)
}

// ListVMDVariables returns VMD-scoped variable names.
func (r *Registry) ListVMDVariables(continueAfter string, maxNames int) NameListResult {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return paginate(r.vmdVarOrder, continueAfter, maxNames)
}

func nvlKey(scope int, domain, itemID string) string {
	return fmt.Sprintf("nvl:%d:%s/%s", scope, domain, itemID)
}

// DefineNVL creates a named variable list. The domain (if domain-scoped)
// must already be registered. Returns an error if the list already exists.
func (r *Registry) DefineNVL(entry *NVLEntry) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if entry.ItemID == "" {
		return fmt.Errorf("servermodel: empty NVL ItemID")
	}
	if entry.Scope == 1 && entry.Domain == "" {
		return fmt.Errorf("servermodel: domain-scoped NVL requires non-empty domain")
	}
	if entry.Scope == 1 && !r.domains[entry.Domain] {
		return fmt.Errorf("servermodel: domain %q not registered", entry.Domain)
	}

	key := nvlKey(entry.Scope, entry.Domain, entry.ItemID)
	if _, exists := r.nvls[key]; exists {
		return fmt.Errorf("servermodel: NVL %q already defined", key)
	}
	r.nvls[key] = entry

	switch entry.Scope {
	case 0: // VMD
		r.vmdNVLOrder = append(r.vmdNVLOrder, entry.ItemID)
		sort.Strings(r.vmdNVLOrder)
	case 1: // Domain
		r.nvlOrder[entry.Domain] = append(r.nvlOrder[entry.Domain], entry.ItemID)
		sort.Strings(r.nvlOrder[entry.Domain])
	}
	return nil
}

// LookupNVL returns the named variable list for the given scope/domain/itemID.
func (r *Registry) LookupNVL(scope int, domain, itemID string) (*NVLEntry, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	e, ok := r.nvls[nvlKey(scope, domain, itemID)]
	return e, ok
}

// DeleteNVL removes a named variable list. Returns true if the list
// existed and was deleted, false if it did not exist.
func (r *Registry) DeleteNVL(scope int, domain, itemID string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	key := nvlKey(scope, domain, itemID)
	entry, ok := r.nvls[key]
	if !ok {
		return false
	}
	if !entry.Deletable {
		return false
	}
	delete(r.nvls, key)

	switch scope {
	case 0:
		r.vmdNVLOrder = removeFromSorted(r.vmdNVLOrder, itemID)
	case 1:
		r.nvlOrder[domain] = removeFromSorted(r.nvlOrder[domain], itemID)
	}
	return true
}

func removeFromSorted(sorted []string, name string) []string {
	for i, n := range sorted {
		if n == name {
			return append(sorted[:i], sorted[i+1:]...)
		}
	}
	return sorted
}

// ListDomainNVLs returns named variable list names in a domain.
func (r *Registry) ListDomainNVLs(domain, continueAfter string, maxNames int) NameListResult {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return paginate(r.nvlOrder[domain], continueAfter, maxNames)
}

// ListVMDNVLs returns VMD-scoped named variable list names.
func (r *Registry) ListVMDNVLs(continueAfter string, maxNames int) NameListResult {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return paginate(r.vmdNVLOrder, continueAfter, maxNames)
}

func paginate(sorted []string, continueAfter string, maxNames int) NameListResult {
	if maxNames <= 0 {
		maxNames = defaultPageSize
	}
	start := 0
	if continueAfter != "" {
		for i, name := range sorted {
			if name == continueAfter {
				start = i + 1
				break
			}
		}
		if start == 0 {
			idx := sort.SearchStrings(sorted, continueAfter)
			start = idx
		}
	}

	remaining := sorted[start:]
	if len(remaining) <= maxNames {
		return NameListResult{Names: remaining, MoreFollows: false}
	}
	return NameListResult{Names: remaining[:maxNames], MoreFollows: true}
}

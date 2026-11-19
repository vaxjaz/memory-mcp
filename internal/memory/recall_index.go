package memory

// This is a lexical index, not a semantic interpreter. The host supplies search
// perspectives and judges the returned evidence against the current task.
import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/vaxjaz/memory-mcp/internal/matching"
	"math"
	"sort"
	"strings"
	"unicode"

	"github.com/vaxjaz/memory-mcp/internal/textguard"
)

type RecallQuery struct {
	Terms []string `json:"terms" jsonschema:"1-16 grounded terms from one host-chosen investigation perspective."`
	Match string   `json:"match,omitempty" jsonschema:"all (default): every term; any: alternatives. Lexical, not semantic."`
}

// Han bigrams support short Chinese terms without a model, external dictionary,
// or SQLite extension. Identifiers remain whole; there is no implicit prefix,
// synonym expansion, negation detection, or language understanding.
func recallTokens(s string) []string {
	var out []string
	var word, han []rune
	flushWord := func() {
		if len(word) > 1 {
			out = append(out, strings.ToLower(string(word)))
		}
		word = nil
	}
	flushHan := func() {
		for i := 0; i+1 < len(han); i++ {
			out = append(out, string(han[i:i+2]))
		}
		han = nil
	}
	for _, r := range s {
		if unicode.Is(unicode.Han, r) {
			flushWord()
			han = append(han, r)
		} else {
			flushHan()
			if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '$' {
				word = append(word, unicode.ToLower(r))
			} else {
				flushWord()
			}
		}
	}
	flushWord()
	flushHan()
	return out
}

func indexRecallEntry(ctx context.Context, tx *sql.Tx, e Entry) error {
	if _, err := tx.ExecContext(ctx, "DELETE FROM recall_documents WHERE entry_id=?", e.ID); err != nil {
		return err
	}
	if strings.HasPrefix(e.SourceRef, HookSourcePrefix) || strings.HasPrefix(e.SourceRef, "maintenance-event:") {
		return nil
	}
	weights := map[string]float64{}
	add := func(text string, weight float64) {
		for _, token := range recallTokens(text) {
			weights[token] += weight
		}
	}
	add(e.Title, 3)
	if len(entryCues(e)) == 0 {
		add(e.Body, 1)
	}
	if e.Profile != nil {
		if x := e.Profile.Experience; x != nil {
			add(x.Applicability, 3)
			add(x.Pitfall, 2)
			add(x.Counterexample, 2)
			add(x.Impact, 2)
			add(x.Boundary, 1)
		} else if x := e.Profile.Agreement; x != nil {
			add(x.Requirement, 3)
			add(x.Authority, 1)
			add(x.Applicability, 3)
			add(x.Boundary, 1)
		} else {
			add(e.Profile.FutureUse, 2)
			add(e.Profile.Applicability, 2)
			add(e.Profile.Boundary, 1)
			add(strings.Join(e.Profile.SearchHints, " "), 3)
		}
	}
	for _, c := range entryCues(e) {
		add(c.Text, 2)
		for _, group := range c.Matches {
			group.VisitEntrances(func(leaf *matching.Expression, value string) {
				add(value, 8)
				compact := humanSignalKey(value)
				if leaf.Normalize == "whitespace" && compact != strings.ToLower(value) {
					add(compact, 8)
				}
			})
		}
	}
	length := 0.0
	for _, n := range weights {
		length += n
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO recall_documents(entry_id,scope,length)VALUES(?,?,?)", e.ID, e.Scope, length); err != nil {
		return err
	}
	stmt, err := tx.PrepareContext(ctx, "INSERT INTO recall_terms(scope,term,entry_id,frequency)VALUES(?,?,?,?)")
	if err != nil {
		return err
	}
	defer stmt.Close()
	for term, n := range weights {
		if _, err = stmt.ExecContext(ctx, e.Scope, term, e.ID, n); err != nil {
			return err
		}
	}
	return indexExperienceEntrances(ctx, tx, e)
}

func migrateRecall(tx *sql.Tx) error {
	if _, err := tx.Exec(`
 CREATE TABLE IF NOT EXISTS recall_documents(entry_id TEXT PRIMARY KEY REFERENCES entries(id) ON DELETE CASCADE,scope TEXT NOT NULL,length REAL NOT NULL);
 CREATE INDEX IF NOT EXISTS recall_documents_scope ON recall_documents(scope);
 CREATE TABLE IF NOT EXISTS recall_terms(scope TEXT NOT NULL,term TEXT NOT NULL,entry_id TEXT NOT NULL REFERENCES recall_documents(entry_id) ON DELETE CASCADE,frequency REAL NOT NULL,PRIMARY KEY(scope,term,entry_id));
 CREATE INDEX IF NOT EXISTS recall_terms_entry ON recall_terms(entry_id);
 CREATE TABLE IF NOT EXISTS experience_keys(scope TEXT NOT NULL,fingerprint TEXT NOT NULL,root_id TEXT NOT NULL REFERENCES entries(id) ON DELETE CASCADE,PRIMARY KEY(scope,fingerprint));
 CREATE TABLE IF NOT EXISTS experience_entrances(entry_id TEXT NOT NULL REFERENCES entries(id) ON DELETE CASCADE,cue_id TEXT NOT NULL,match_group INTEGER NOT NULL,value TEXT NOT NULL,match_key TEXT NOT NULL,scope TEXT NOT NULL,PRIMARY KEY(entry_id,cue_id,match_group,value));
 CREATE INDEX IF NOT EXISTS experience_entrances_scope ON experience_entrances(scope);
 CREATE TABLE IF NOT EXISTS recall_index_version(id INTEGER PRIMARY KEY CHECK(id=1),version INTEGER NOT NULL);
 `); err != nil {
		return err
	}
	// Rebuild derived data so removed selectors cannot survive migration.
	if _, err := tx.Exec("DELETE FROM experience_keys; DELETE FROM experience_entrances;"); err != nil {
		return err
	}
	after := ""
	for {
		rows, err := tx.Query("SELECT "+qualifiedEntryColumns("e")+",coalesce(p.profile_json,'') FROM entries e LEFT JOIN entry_profiles p ON p.entry_id=e.id WHERE e.id>? ORDER BY e.id LIMIT 64", after)
		if err != nil {
			return err
		}
		var batch []Entry
		for rows.Next() {
			var e Entry
			var profile string
			if err = rows.Scan(&e.ID, &e.RootID, &e.PreviousID, &e.Kind, &e.Scope, &e.Title, &e.Body, &e.Origin, &e.SourceRef, &e.Host, &e.Model, &e.Session, &e.CreatedAt, &e.Current, &e.NeedsReview, &profile); err != nil {
				rows.Close()
				return err
			}
			if profile != "" {
				if err = json.Unmarshal([]byte(profile), &e.Profile); err != nil {
					rows.Close()
					return err
				}
			}
			batch = append(batch, e)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		for _, e := range batch {
			if err = indexRecallEntry(context.Background(), tx, e); err != nil {
				return err
			}
			after = e.ID
		}
		if len(batch) < 64 {
			break
		}
	}
	_, err := tx.Exec("INSERT INTO recall_index_version(id,version)VALUES(1,4) ON CONFLICT(id)DO UPDATE SET version=excluded.version")
	return err
}

func qualifiedEntryColumns(alias string) string {
	return alias + "." + strings.ReplaceAll(columns, ",", ","+alias+".")
}

func normalizeRecallQueries(in ExperienceLookup) ([]RecallQuery, error) {
	queries := in.Queries
	if len(queries) == 0 {
		_, terms, err := normalizeSearchQuery(strings.Join(in.Keywords, " "))
		if err != nil {
			return nil, err
		}
		if len(terms) == 0 {
			return nil, errors.New("supply host-generated queries, or compatibility keywords")
		}
		queries = []RecallQuery{{Terms: terms, Match: "any"}}
	}
	if len(queries) > 4 {
		return nil, errors.New("at most four independent search perspectives")
	}
	var out []RecallQuery
	seen := map[string]bool{}
	total := 0
	for _, q := range queries {
		if q.Match == "" {
			q.Match = "all"
		}
		if q.Match != "all" && q.Match != "any" {
			return nil, errors.New("query.match must be all or any")
		}
		if len(q.Terms) == 0 || (len(in.Queries) > 0 && len(q.Terms) > 16) {
			return nil, errors.New("each query needs 1-16 informative terms")
		}
		terms := []string{}
		local := map[string]bool{}
		for _, term := range q.Terms {
			term = strings.ToLower(strings.TrimSpace(term))
			total += len(term)
			if term == "" || len(term) > 256 || len(recallTermOptions(term)) == 0 {
				return nil, errors.New("query term is empty, oversized or has no indexable identifier/Chinese pair")
			}
			if err := textguard.Check("query.term", term, false); err != nil {
				return nil, err
			}
			if !local[term] {
				terms = append(terms, term)
				local[term] = true
			}
		}
		sort.Strings(terms)
		q.Terms = terms
		b, _ := json.Marshal(q)
		if !seen[string(b)] {
			out = append(out, q)
			seen[string(b)] = true
		}
	}
	if total > 4096 {
		return nil, errors.New("combined query terms exceed 4096 bytes")
	}
	return out, nil
}

type recallRank struct {
	Entry   Entry
	Queries []int
	Terms   []string
	Score   float64
	Exact   int
	Forced  bool
}

func (s *Store) rankedRecall(ctx context.Context, scope string, queries []RecallQuery) ([]recallRank, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	allTokens := map[string]bool{}
	for _, q := range queries {
		for _, term := range q.Terms {
			for _, option := range recallTermOptions(term) {
				for _, token := range option {
					allTokens[token] = true
				}
			}
		}
	}
	keys := []string{}
	for k := range allTokens {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	raw, _ := json.Marshal(keys)
	rows, err := tx.QueryContext(ctx, `SELECT t.entry_id,t.term,t.frequency,d.length FROM recall_terms t JOIN recall_documents d ON d.entry_id=t.entry_id JOIN entries e ON e.id=t.entry_id
 WHERE t.scope=? AND t.term IN(SELECT value FROM json_each(?)) AND e.current=1
 AND NOT EXISTS(SELECT 1 FROM managed_entries m WHERE m.root_id=e.root_id AND m.state<>'active')
 ORDER BY t.entry_id,t.term`, scope, string(raw))
	if err != nil {
		return nil, err
	}
	type doc struct {
		terms  map[string]float64
		length float64
	}
	docs := map[string]*doc{}
	df := map[string]int{}
	for rows.Next() {
		var id, term string
		var n, length float64
		if err = rows.Scan(&id, &term, &n, &length); err != nil {
			rows.Close()
			return nil, err
		}
		d := docs[id]
		if d == nil {
			d = &doc{terms: map[string]float64{}, length: length}
			docs[id] = d
		}
		d.terms[term] = n
		df[term]++
		if len(docs) > 2000 {
			rows.Close()
			return nil, errors.New("more than 2000 candidates; narrow the business object or query conditions")
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	var corpus int
	var average float64
	if err = tx.QueryRowContext(ctx, "SELECT count(*),coalesce(avg(d.length),1) FROM recall_documents d JOIN entries e ON e.id=d.entry_id WHERE d.scope=? AND e.current=1", scope).Scan(&corpus, &average); err != nil {
		return nil, err
	}
	if average < 1 {
		average = 1
	}
	ranked := map[string]*recallRank{}
	for qi, q := range queries {
		type hit struct {
			id    string
			score float64
			terms []string
		}
		var hits []hit
		for id, d := range docs {
			found := []string{}
			score := 0.0
			used := map[string]bool{}
			for _, term := range q.Terms {
				tokens, complete := matchedRecallTokens(term, d.terms)
				if !complete {
					continue
				}
				found = append(found, term)
				for _, token := range tokens {
					if used[token] {
						continue
					}
					used[token] = true
					tf := d.terms[token]
					idf := math.Log(1 + (float64(corpus-df[token])+0.5)/(float64(df[token])+0.5))
					score += idf * (tf * 2.2) / (tf + 1.2*(0.25+0.75*d.length/average))
				}
			}
			if len(found) == 0 || (q.Match == "all" && len(found) != len(q.Terms)) {
				continue
			}
			hits = append(hits, hit{id, score, found})
		}
		sort.Slice(hits, func(i, j int) bool {
			if hits[i].score != hits[j].score {
				return hits[i].score > hits[j].score
			}
			return hits[i].id < hits[j].id
		})
		for pos, h := range hits {
			r := ranked[h.id]
			if r == nil {
				r = &recallRank{}
				r.Entry.ID = h.id
				ranked[h.id] = r
			}
			r.Score += 1.0 / (60 + float64(pos+1))
			r.Queries = append(r.Queries, qi)
			r.Terms = append(r.Terms, h.terms...)
		}
	}
	result := []recallRank{}
	for id, r := range ranked {
		e, er := scanEntry(tx.QueryRowContext(ctx, "SELECT "+columns+" FROM entries WHERE id=?", id))
		if er != nil {
			return nil, er
		}
		if er = loadGovernance(ctx, tx, &e); er != nil {
			return nil, er
		}
		r.Entry = e
		if !e.Current || e.State.State != "active" || profileExpired(e.Profile) {
			continue
		}
		var entrances []string
		for _, cue := range entryCues(e) {
			for _, group := range cue.Matches {
				group.VisitEntrances(func(_ *matching.Expression, value string) { entrances = append(entrances, value) })
			}
		}
		uniq := map[string]bool{}
		r.Terms = nil
		for _, qi := range r.Queries {
			for _, term := range queries[qi].Terms {
				_, complete := matchedRecallTokens(term, docs[id].terms)
				if complete && !uniq[term] {
					uniq[term] = true
					r.Terms = append(r.Terms, term)
					for _, a := range entrances {
						if strings.EqualFold(a, term) {
							r.Exact++
						}
					}
				}
			}
		}
		result = append(result, *r)
	}
	sortRecall(result)
	return result, tx.Commit()
}

func sortRecall(items []recallRank) {
	sort.SliceStable(items, func(i, j int) bool {
		a, b := items[i], items[j]
		if a.Forced != b.Forced {
			return a.Forced
		}
		if a.Exact != b.Exact {
			return a.Exact > b.Exact
		}
		if len(a.Queries) != len(b.Queries) {
			return len(a.Queries) > len(b.Queries)
		}
		if a.Score != b.Score {
			return a.Score > b.Score
		}
		return a.Entry.ID < b.Entry.ID
	})
}

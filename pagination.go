package flashpi

import (
	"context"
	"encoding/json"
	"iter"
	"maps"
	"net/url"
)

// Page is one page of a list, newest first (establishments and emission points: by code). E is
// the type of its items: *Document, *Issuer, ..., or Event.
//
// The List methods walk every page for you; ListPage returns one page at a time:
//
//	page, err := client.Documents.ListPage(ctx, &flashpi.DocumentListParams{Limit: new(50)})
//	for page != nil && err == nil {
//		for _, document := range page.Data { ... }
//		page, err = page.NextPage(ctx)
//	}
type Page[E any] struct {
	// Data holds the items of this page.
	Data []E

	// HasMore reports whether more items follow this page.
	HasMore bool

	// NextCursor is the cursor of the next page, or "" on the last one.
	NextCursor string

	fetch func(ctx context.Context, cursor string) (*Page[E], error)
}

// HasNextPage reports whether NextPage returns a page.
func (p *Page[E]) HasNextPage() bool {
	return p != nil && p.HasMore && p.NextCursor != ""
}

// NextPage reads the next page, with the same filters and options. After the last page it
// returns nil and no error.
func (p *Page[E]) NextPage(ctx context.Context) (*Page[E], error) {
	if !p.HasNextPage() || p.fetch == nil {
		return nil, nil
	}
	return p.fetch(ctx, p.NextCursor)
}

// lister reads the pages of one list operation.
type lister[E any] struct {
	engine  *engine
	op      *operation
	options []RequestOption
	decode  func(json.RawMessage) (E, error)
}

// page reads the page after cursor ("" for the first one).
func (l *lister[E]) page(ctx context.Context, cursor string) (*Page[E], error) {
	op := *l.op
	if cursor != "" {
		op.query = maps.Clone(op.query)
		if op.query == nil {
			op.query = url.Values{}
		}
		op.query.Set("cursor", cursor)
	}
	page := &Page[E]{fetch: l.page}
	err := l.engine.do(ctx, &op, l.options, func(a *answer) error { return l.read(a, page) })
	if err != nil {
		return nil, err
	}
	return page, nil
}

// read decodes the body of a list: its data and pagination.
func (l *lister[E]) read(a *answer, page *Page[E]) error {
	var body struct {
		Data       []json.RawMessage          `json:"data"`
		Pagination map[string]json.RawMessage `json:"pagination"`
	}
	if err := readJSON(a, &body); err != nil {
		return err
	}
	if body.Data == nil || body.Pagination == nil {
		return &Error{Message: "The API answered a list without data and pagination"}
	}
	page.Data = make([]E, 0, len(body.Data))
	for _, raw := range body.Data {
		item, err := l.decode(raw)
		if err != nil {
			return &Error{Message: "The API answered a list item this version of the SDK cannot read: " + err.Error(), Err: err}
		}
		page.Data = append(page.Data, item)
	}
	page.HasMore = string(body.Pagination["hasMore"]) == "true"
	var cursor string
	if json.Unmarshal(body.Pagination["nextCursor"], &cursor) == nil {
		page.NextCursor = cursor
	}
	return nil
}

// all walks every item of every page, reading the pages as it goes.
func (l *lister[E]) all(ctx context.Context) iter.Seq2[E, error] {
	return func(yield func(E, error) bool) {
		page, err := l.page(ctx, "")
		for {
			if err != nil {
				var zero E
				yield(zero, err)
				return
			}
			for _, item := range page.Data {
				if !yield(item, nil) {
					return
				}
			}
			if !page.HasNextPage() {
				return
			}
			page, err = page.NextPage(ctx)
		}
	}
}

// decodeStruct decodes one item of a list into a new T.
func decodeStruct[T any](raw json.RawMessage) (*T, error) {
	item := new(T)
	if err := json.Unmarshal(raw, item); err != nil {
		return nil, err
	}
	return item, nil
}

// newLister is the lister of a GET list operation.
func newLister[E any](e *engine, path string, query url.Values, options []RequestOption, decode func(json.RawMessage) (E, error)) *lister[E] {
	return &lister[E]{
		engine:  e,
		op:      &operation{method: "GET", path: path, query: query, accept: jsonAccept},
		options: options,
		decode:  decode,
	}
}

// failedList is the iterator of a list whose arguments were refused: it yields the error once.
func failedList[E any](err error) iter.Seq2[E, error] {
	return func(yield func(E, error) bool) {
		var zero E
		yield(zero, err)
	}
}

package sync

import (
	"sort"
	"strings"

	"github.com/sergi/go-diff/diffmatchpatch"
)

type textEdit struct {
	start int
	end   int
	text  string
}

// MergeText attempts a conservative three-way text merge.
func MergeText(base, local, server string) (string, bool) {
	if local == server {
		return local, true
	}
	if base == local {
		return server, true
	}
	if base == server {
		return local, true
	}

	edits := append(diffEdits(base, local), diffEdits(base, server)...)
	sort.Slice(edits, func(i, j int) bool {
		if edits[i].start != edits[j].start {
			return edits[i].start < edits[j].start
		}
		if edits[i].end != edits[j].end {
			return edits[i].end < edits[j].end
		}
		return edits[i].text < edits[j].text
	})

	edits, ok := normalizeTextEdits(edits)
	if !ok {
		return "", false
	}

	var out strings.Builder
	pos := 0
	for _, edit := range edits {
		if edit.start < pos {
			return "", false
		}
		out.WriteString(base[pos:edit.start])
		out.WriteString(edit.text)
		pos = edit.end
	}
	out.WriteString(base[pos:])

	return out.String(), true
}

func diffEdits(base, changed string) []textEdit {
	dmp := diffmatchpatch.New()
	diffs := dmp.DiffMain(base, changed, false)

	var edits []textEdit
	basePos := 0
	inEdit := false
	edit := textEdit{}

	flush := func() {
		if !inEdit {
			return
		}
		edits = append(edits, edit)
		inEdit = false
		edit = textEdit{}
	}

	for _, diff := range diffs {
		switch diff.Type {
		case diffmatchpatch.DiffEqual:
			flush()
			basePos += len(diff.Text)
		case diffmatchpatch.DiffDelete:
			if !inEdit {
				inEdit = true
				edit.start = basePos
				edit.end = basePos
			}
			basePos += len(diff.Text)
			edit.end = basePos
		case diffmatchpatch.DiffInsert:
			if !inEdit {
				inEdit = true
				edit.start = basePos
				edit.end = basePos
			}
			edit.text += diff.Text
		}
	}
	flush()

	return edits
}

func normalizeTextEdits(edits []textEdit) ([]textEdit, bool) {
	normalized := make([]textEdit, 0, len(edits))
	for _, edit := range edits {
		if len(normalized) == 0 {
			normalized = append(normalized, edit)
			continue
		}

		last := normalized[len(normalized)-1]
		if last.start == last.end && edit.start == edit.end && last.start == edit.start {
			if last.text != edit.text {
				return nil, false
			}
			continue
		}
		if insertBoundaryConflict(last, edit) {
			return nil, false
		}
		if last.end > edit.start {
			return nil, false
		}

		normalized = append(normalized, edit)
	}

	return normalized, true
}

func insertBoundaryConflict(a, b textEdit) bool {
	if !a.insertOnly() && !b.insertOnly() {
		return false
	}
	if a.insertOnly() && b.insertOnly() {
		return false
	}

	insert := a
	span := b
	if b.insertOnly() {
		insert = b
		span = a
	}

	return insert.start == span.start || insert.start == span.end
}

func (edit textEdit) insertOnly() bool {
	return edit.start == edit.end
}

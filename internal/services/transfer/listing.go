package transfer

import (
	"github.com/UrielJaloto/paygo/internal/domain/apikey"
	"github.com/UrielJaloto/paygo/internal/domain/page"
	"github.com/UrielJaloto/paygo/internal/domain/transfer"
)

func parsePage(cursorText, limitText string) (page.Cursor, page.Limit, error) {
	cursor, err := page.ParseCursor(cursorText)
	if err != nil {
		return page.Cursor{}, 0, err
	}
	limit, err := page.ParseLimit(limitText)
	if err != nil {
		return page.Cursor{}, 0, err
	}
	return cursor, limit, nil
}

func buildPage(found []transfer.Transfer, owner apikey.OwnerID, limit page.Limit) TransferPage {
	kept, next := found, ""
	if len(found) > int(limit) {
		kept = found[:limit]
		next = page.NewCursor(string(kept[len(kept)-1].ID())).Encode()
	}
	items := make([]TransferOutput, 0, len(kept))
	for _, t := range kept {
		if t.OwnerID() == owner {
			items = append(items, toOutput(t))
		}
	}
	if len(items) == 0 {
		next = ""
	}
	return TransferPage{Items: items, NextCursor: next}
}

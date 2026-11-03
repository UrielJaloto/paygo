package transfer

import (
	"context"
	"fmt"

	"github.com/UrielJaloto/paygo/internal/domain/apikey"
	"github.com/UrielJaloto/paygo/internal/domain/transfer"
)

type reversal struct {
	owner apikey.OwnerID
	key   transfer.IdempotencyKey
	id    transfer.ID
}

func (s *Service) reverse(ctx context.Context, store Store, target reversal) (TransferOutput, error) {
	original, err := store.Transfers.FindByIDForUpdate(ctx, target.id)
	if err != nil {
		return TransferOutput{}, fmt.Errorf("finding transfer: %w", err)
	}
	if original.OwnerID() != target.owner {
		return TransferOutput{}, fmt.Errorf("%w: %s", transfer.ErrTransferNotFound, target.id)
	}
	request, err := original.ReversalRequest(target.key)
	if err != nil {
		return TransferOutput{}, err
	}
	replayed, found, err := findReplay(ctx, store, request)
	if err != nil || found {
		return replayed, err
	}
	if err := original.Reverse(); err != nil {
		return TransferOutput{}, err
	}
	output, err := s.move(ctx, store, request)
	if err != nil {
		return TransferOutput{}, err
	}
	if err := store.Transfers.MarkReversed(ctx, original); err != nil {
		return TransferOutput{}, fmt.Errorf("marking transfer reversed: %w", err)
	}
	return output, nil
}

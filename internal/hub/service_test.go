package hub

import (
	"context"
	"errors"
	"testing"
	"time"
)

type mockRepository struct {
	listPublicFn func(context.Context, int) ([]PublicLecture, error)
}

func (m *mockRepository) ListPublic(ctx context.Context, limit int) ([]PublicLecture, error) {
	return m.listPublicFn(ctx, limit)
}

func TestService_List_PassThrough(t *testing.T) {
	now := time.Now()
	repo := &mockRepository{
		listPublicFn: func(_ context.Context, limit int) ([]PublicLecture, error) {
			if limit != 25 {
				t.Errorf("limit = %d, ожидается 25", limit)
			}
			return []PublicLecture{{ID: "lecture-1", Title: "Алгебра", PublishedAt: now}}, nil
		},
	}

	lectures, err := NewService(repo, 25).List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(lectures) != 1 || lectures[0].ID != "lecture-1" {
		t.Errorf("List = %+v, ожидается одна лекция", lectures)
	}
}

func TestService_List_Empty(t *testing.T) {
	repo := &mockRepository{
		listPublicFn: func(_ context.Context, _ int) ([]PublicLecture, error) {
			return []PublicLecture{}, nil
		},
	}

	lectures, err := NewService(repo, 0).List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if lectures == nil || len(lectures) != 0 {
		t.Errorf("List = %#v, ожидается пустой ненулевой срез", lectures)
	}
}

func TestService_List_RepoError(t *testing.T) {
	wantErr := errors.New("репозиторий недоступен")
	repo := &mockRepository{
		listPublicFn: func(_ context.Context, _ int) ([]PublicLecture, error) {
			return nil, wantErr
		},
	}

	_, err := NewService(repo, 0).List(context.Background())
	if !errors.Is(err, wantErr) {
		t.Errorf("List error = %v, ожидается %v", err, wantErr)
	}
}

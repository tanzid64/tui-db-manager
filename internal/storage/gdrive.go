package storage

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"
)

type driveFile struct {
	ID      string
	Name    string
	Size    int64
	ModTime time.Time
}

// driveAPI is the internal seam over google.golang.org/api/drive. The backend's
// key/name/prefix logic is tested against a fake implementation.
type driveAPI interface {
	Create(ctx context.Context, name, parent string, r io.Reader) error
	List(ctx context.Context, parent string) ([]driveFile, error)
	Download(ctx context.Context, id string) (io.ReadCloser, error)
	Delete(ctx context.Context, id string) error
	Ping(ctx context.Context, parent string) error
}

type GDrive struct {
	api      driveAPI
	folderID string
}

var _ Backend = (*GDrive)(nil)

// NewGDriveWith builds a backend over a given driveAPI (used by both the real
// constructor and tests).
func NewGDriveWith(api driveAPI, folderID string) *GDrive {
	return &GDrive{api: api, folderID: folderID}
}

func (g *GDrive) Name() string { return "gdrive" }

func (g *GDrive) Put(ctx context.Context, key string, r io.Reader, size int64) error {
	// Drive does not enforce unique names, so Create alone would accumulate
	// duplicates on a repeated key and make Get/Delete/retention ambiguous.
	// Delete any existing files with this name first, so a Put replaces the key
	// (matching the local and S3 backends' contract).
	files, err := g.api.List(ctx, g.folderID)
	if err != nil {
		return err
	}
	for _, f := range files {
		if f.Name == key {
			if err := g.api.Delete(ctx, f.ID); err != nil {
				return err
			}
		}
	}
	return g.api.Create(ctx, key, g.folderID, r)
}

func (g *GDrive) findByName(ctx context.Context, key string) (driveFile, error) {
	files, err := g.api.List(ctx, g.folderID)
	if err != nil {
		return driveFile{}, err
	}
	for _, f := range files {
		if f.Name == key {
			return f, nil
		}
	}
	return driveFile{}, fmt.Errorf("gdrive: object %q not found", key)
}

func (g *GDrive) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	f, err := g.findByName(ctx, key)
	if err != nil {
		return nil, err
	}
	return g.api.Download(ctx, f.ID)
}

func (g *GDrive) List(ctx context.Context, prefix string) ([]Object, error) {
	files, err := g.api.List(ctx, g.folderID)
	if err != nil {
		return nil, err
	}
	var out []Object
	for _, f := range files {
		if strings.HasPrefix(f.Name, prefix) {
			out = append(out, Object{Key: f.Name, Size: f.Size, ModTime: f.ModTime})
		}
	}
	return out, nil
}

func (g *GDrive) Delete(ctx context.Context, key string) error {
	f, err := g.findByName(ctx, key)
	if err != nil {
		return err
	}
	return g.api.Delete(ctx, f.ID)
}

func (g *GDrive) Stat(ctx context.Context, key string) (Object, error) {
	f, err := g.findByName(ctx, key)
	if err != nil {
		return Object{}, err
	}
	return Object{Key: f.Name, Size: f.Size, ModTime: f.ModTime}, nil
}

func (g *GDrive) Test(ctx context.Context) error {
	if g.folderID == "" {
		return fmt.Errorf("gdrive: folder_id is required")
	}
	return g.api.Ping(ctx, g.folderID)
}

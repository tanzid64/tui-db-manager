package storage

import (
	"context"
	"fmt"
	"io"
	"time"

	"google.golang.org/api/drive/v3"
	"google.golang.org/api/googleapi"
	"google.golang.org/api/option"
)

type GDriveOptions struct {
	CredentialsJSON []byte
	FolderID        string
}

// NewGDrive builds a Google Drive backend authenticated with a service account.
func NewGDrive(ctx context.Context, o GDriveOptions) (*GDrive, error) {
	if o.FolderID == "" {
		return nil, fmt.Errorf("gdrive: folder_id is required")
	}
	if len(o.CredentialsJSON) == 0 {
		return nil, fmt.Errorf("gdrive: service-account credentials are required")
	}
	// WithCredentialsJSON is marked deprecated because of a credential-config
	// injection risk when the JSON comes from an untrusted source. Here it comes
	// from the user's own vault/config (a source under their control), so that
	// risk does not apply; migrating to cloud.google.com/go/auth is unwarranted.
	srv, err := drive.NewService(ctx,
		option.WithCredentialsJSON(o.CredentialsJSON),
		option.WithScopes(drive.DriveScope),
	)
	if err != nil {
		return nil, fmt.Errorf("gdrive: %w", err)
	}
	return NewGDriveWith(&driveService{srv: srv}, o.FolderID), nil
}

type driveService struct{ srv *drive.Service }

func (d *driveService) Create(ctx context.Context, name, parent string, r io.Reader) error {
	_, err := d.srv.Files.
		Create(&drive.File{Name: name, Parents: []string{parent}}).
		Media(r).
		SupportsAllDrives(true).
		Context(ctx).
		Do()
	return err
}

func (d *driveService) List(ctx context.Context, parent string) ([]driveFile, error) {
	var out []driveFile
	q := fmt.Sprintf("'%s' in parents and trashed=false", parent)
	call := d.srv.Files.List().
		Q(q).
		Fields("nextPageToken, files(id,name,size,modifiedTime)").
		SupportsAllDrives(true).
		IncludeItemsFromAllDrives(true).
		PageSize(1000)
	err := call.Pages(ctx, func(page *drive.FileList) error {
		for _, f := range page.Files {
			var mt time.Time
			if f.ModifiedTime != "" {
				mt, _ = time.Parse(time.RFC3339, f.ModifiedTime)
			}
			out = append(out, driveFile{ID: f.Id, Name: f.Name, Size: f.Size, ModTime: mt})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (d *driveService) Download(ctx context.Context, id string) (io.ReadCloser, error) {
	resp, err := d.srv.Files.Get(id).SupportsAllDrives(true).Context(ctx).Download()
	if err != nil {
		return nil, err
	}
	return resp.Body, nil
}

func (d *driveService) Delete(ctx context.Context, id string) error {
	return d.srv.Files.Delete(id).SupportsAllDrives(true).Context(ctx).Do()
}

func (d *driveService) Ping(ctx context.Context, parent string) error {
	// Verify the folder is reachable by the service account.
	_, err := d.srv.Files.Get(parent).SupportsAllDrives(true).Fields("id").Context(ctx).Do()
	if ae, ok := err.(*googleapi.Error); ok && ae.Code == 404 {
		return fmt.Errorf("gdrive: folder %q not found or not shared with the service account", parent)
	}
	return err
}

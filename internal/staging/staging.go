// Package staging provides file staging for Workspace, Shock, and local storage.
package staging

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/BV-BRC/cwe-cwl/internal/config"
	"github.com/BV-BRC/cwe-cwl/internal/cwl"
)

// Backend represents a storage backend type.
type Backend string

const (
	BackendLocal     Backend = "local"
	BackendWorkspace Backend = "workspace"
	BackendShock     Backend = "shock"
)

// FileRef represents a reference to a file in any backend.
type FileRef struct {
	Backend  Backend `json:"backend"`
	Path     string  `json:"path"`     // Path in Workspace or local FS
	NodeID   string  `json:"node_id"`  // Shock node ID
	Size     int64   `json:"size"`
	Checksum string  `json:"checksum"`
}

// Stager handles file staging between backends.
type Stager struct {
	config     *config.StorageConfig
	httpClient *http.Client
	token      string
}

// NewStager creates a new file stager.
func NewStager(cfg *config.StorageConfig) *Stager {
	return &Stager{
		config: cfg,
		httpClient: &http.Client{
			Timeout: 5 * time.Minute,
		},
	}
}

// SetToken sets the authentication token for API calls.
func (s *Stager) SetToken(token string) {
	s.token = token
}

// ParseFileRef parses a file reference from a CWL File value.
func (s *Stager) ParseFileRef(file interface{}) (*FileRef, error) {
	switch v := file.(type) {
	case map[string]interface{}:
		class, _ := v["class"].(string)
		if class != cwl.TypeFile && class != cwl.TypeDirectory {
			return nil, fmt.Errorf("not a File or Directory: %s", class)
		}

		ref := &FileRef{}

		// Get path or location
		if path, ok := v["path"].(string); ok {
			ref.Path = path
		} else if loc, ok := v["location"].(string); ok {
			ref.Path = loc
		}

		if ref.Path == "" {
			return nil, fmt.Errorf("file has no path or location")
		}

		// Determine backend
		ref.Backend = s.detectBackend(ref.Path)

		// Extract Shock node ID if applicable
		if strings.HasPrefix(ref.Path, "shock://") {
			parts := strings.Split(ref.Path, "/")
			if len(parts) >= 5 {
				ref.NodeID = parts[len(parts)-1]
			}
		}

		// Get optional metadata
		if size, ok := v["size"].(float64); ok {
			ref.Size = int64(size)
		}
		if checksum, ok := v["checksum"].(string); ok {
			ref.Checksum = checksum
		}

		return ref, nil

	case string:
		ref := &FileRef{
			Path:    v,
			Backend: s.detectBackend(v),
		}
		return ref, nil

	default:
		return nil, fmt.Errorf("unsupported file type: %T", file)
	}
}

// detectBackend determines the storage backend from a path.
func (s *Stager) detectBackend(path string) Backend {
	if strings.HasPrefix(path, "shock://") {
		return BackendShock
	}
	if strings.HasPrefix(path, "/") {
		// Check if it's a local path or Workspace path
		if strings.HasPrefix(path, s.config.LocalPath) {
			return BackendLocal
		}
		// Assume Workspace for other absolute paths
		return BackendWorkspace
	}
	// Relative paths are local
	return BackendLocal
}

// Validate checks if a file exists and is accessible.
func (s *Stager) Validate(ctx context.Context, ref *FileRef) error {
	switch ref.Backend {
	case BackendLocal:
		return s.validateLocal(ref)
	case BackendWorkspace:
		return s.validateWorkspace(ctx, ref)
	case BackendShock:
		return s.validateShock(ctx, ref)
	default:
		return fmt.Errorf("unknown backend: %s", ref.Backend)
	}
}

// validateLocal validates a local file.
func (s *Stager) validateLocal(ref *FileRef) error {
	info, err := os.Stat(ref.Path)
	if err != nil {
		return err
	}
	ref.Size = info.Size()
	return nil
}

// validateWorkspace validates a Workspace file.
func (s *Stager) validateWorkspace(ctx context.Context, ref *FileRef) error {
	req, err := http.NewRequestWithContext(ctx, "GET", s.config.WorkspaceURL+"/stat"+ref.Path, nil)
	if err != nil {
		return err
	}

	req.Header.Set("Authorization", s.token)
	req.Header.Set("Accept", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("workspace API error: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return fmt.Errorf("file not found: %s", ref.Path)
	}
	if resp.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf("unauthorized access to: %s", ref.Path)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("workspace error: %d", resp.StatusCode)
	}

	// Parse response to get file info
	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return err
	}

	if size, ok := result["size"].(float64); ok {
		ref.Size = int64(size)
	}
	if checksum, ok := result["checksum"].(string); ok {
		ref.Checksum = checksum
	}

	return nil
}

// validateShock validates a Shock node.
func (s *Stager) validateShock(ctx context.Context, ref *FileRef) error {
	url := fmt.Sprintf("%s/node/%s", s.config.ShockURL, ref.NodeID)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return err
	}

	req.Header.Set("Authorization", "OAuth "+s.token)
	req.Header.Set("Accept", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("shock API error: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return fmt.Errorf("shock node not found: %s", ref.NodeID)
	}
	if resp.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf("unauthorized access to shock node: %s", ref.NodeID)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("shock error: %d", resp.StatusCode)
	}

	// Parse response
	var result struct {
		Data struct {
			File struct {
				Size     int64  `json:"size"`
				Checksum string `json:"checksum"`
			} `json:"file"`
		} `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return err
	}

	ref.Size = result.Data.File.Size
	ref.Checksum = result.Data.File.Checksum

	return nil
}

// Stage copies a file to a target location.
func (s *Stager) Stage(ctx context.Context, ref *FileRef, targetPath string) error {
	switch ref.Backend {
	case BackendLocal:
		return s.stageFromLocal(ref, targetPath)
	case BackendWorkspace:
		return s.stageFromWorkspace(ctx, ref, targetPath)
	case BackendShock:
		return s.stageFromShock(ctx, ref, targetPath)
	default:
		return fmt.Errorf("unknown backend: %s", ref.Backend)
	}
}

// stageFromLocal copies a local file.
func (s *Stager) stageFromLocal(ref *FileRef, targetPath string) error {
	src, err := os.Open(ref.Path)
	if err != nil {
		return err
	}
	defer src.Close()

	if err := os.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
		return err
	}

	dst, err := os.Create(targetPath)
	if err != nil {
		return err
	}
	defer dst.Close()

	_, err = io.Copy(dst, src)
	return err
}

// stageFromWorkspace downloads a file from Workspace.
func (s *Stager) stageFromWorkspace(ctx context.Context, ref *FileRef, targetPath string) error {
	req, err := http.NewRequestWithContext(ctx, "GET", s.config.WorkspaceURL+"/download"+ref.Path, nil)
	if err != nil {
		return err
	}

	req.Header.Set("Authorization", s.token)

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("workspace download error: %d", resp.StatusCode)
	}

	if err := os.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
		return err
	}

	dst, err := os.Create(targetPath)
	if err != nil {
		return err
	}
	defer dst.Close()

	_, err = io.Copy(dst, resp.Body)
	return err
}

// stageFromShock downloads a file from Shock.
func (s *Stager) stageFromShock(ctx context.Context, ref *FileRef, targetPath string) error {
	url := fmt.Sprintf("%s/node/%s?download", s.config.ShockURL, ref.NodeID)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return err
	}

	req.Header.Set("Authorization", "OAuth "+s.token)

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("shock download error: %d", resp.StatusCode)
	}

	if err := os.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
		return err
	}

	dst, err := os.Create(targetPath)
	if err != nil {
		return err
	}
	defer dst.Close()

	_, err = io.Copy(dst, resp.Body)
	return err
}

// Upload uploads a file to a backend.
func (s *Stager) Upload(ctx context.Context, localPath string, targetBackend Backend, targetPath string) (*FileRef, error) {
	switch targetBackend {
	case BackendLocal:
		return s.uploadToLocal(localPath, targetPath)
	case BackendWorkspace:
		return s.uploadToWorkspace(ctx, localPath, targetPath)
	case BackendShock:
		return s.uploadToShock(ctx, localPath)
	default:
		return nil, fmt.Errorf("unknown backend: %s", targetBackend)
	}
}

// uploadToLocal copies a file locally.
func (s *Stager) uploadToLocal(localPath, targetPath string) (*FileRef, error) {
	if err := s.stageFromLocal(&FileRef{Path: localPath}, targetPath); err != nil {
		return nil, err
	}

	info, _ := os.Stat(targetPath)
	return &FileRef{
		Backend: BackendLocal,
		Path:    targetPath,
		Size:    info.Size(),
	}, nil
}

// uploadToWorkspace uploads a file to Workspace.
func (s *Stager) uploadToWorkspace(ctx context.Context, localPath, targetPath string) (*FileRef, error) {
	file, err := os.Open(localPath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, "PUT", s.config.WorkspaceURL+"/upload"+targetPath, file)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", s.token)
	req.Header.Set("Content-Type", "application/octet-stream")
	req.ContentLength = info.Size()

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("workspace upload error: %d", resp.StatusCode)
	}

	return &FileRef{
		Backend: BackendWorkspace,
		Path:    targetPath,
		Size:    info.Size(),
	}, nil
}

// uploadToShock uploads a file to Shock.
func (s *Stager) uploadToShock(ctx context.Context, localPath string) (*FileRef, error) {
	file, err := os.Open(localPath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	// Create multipart form for Shock upload
	// This is a simplified version - real implementation would use multipart
	req, err := http.NewRequestWithContext(ctx, "POST", s.config.ShockURL+"/node", file)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "OAuth "+s.token)
	req.Header.Set("Content-Type", "application/octet-stream")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("shock upload error: %d", resp.StatusCode)
	}

	var result struct {
		Data struct {
			ID   string `json:"id"`
			File struct {
				Size int64 `json:"size"`
			} `json:"file"`
		} `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	return &FileRef{
		Backend: BackendShock,
		NodeID:  result.Data.ID,
		Path:    fmt.Sprintf("shock://%s/node/%s", s.config.ShockURL, result.Data.ID),
		Size:    result.Data.File.Size,
	}, nil
}

// ToCWLFile converts a FileRef to a CWL File value.
func (ref *FileRef) ToCWLFile() cwl.FileValue {
	basename := filepath.Base(ref.Path)
	nameroot := strings.TrimSuffix(basename, filepath.Ext(basename))
	nameext := filepath.Ext(basename)

	return cwl.FileValue{
		Class:    cwl.TypeFile,
		Location: ref.Path,
		Path:     ref.Path,
		Basename: basename,
		Dirname:  filepath.Dir(ref.Path),
		Nameroot: nameroot,
		Nameext:  nameext,
		Size:     ref.Size,
		Checksum: ref.Checksum,
	}
}

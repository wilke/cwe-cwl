package executor

import (
	"testing"

	"github.com/BV-BRC/cwe-cwl/internal/cwl"
)

func TestValidateContainerRequirement(t *testing.T) {
	testCases := []struct {
		name    string
		doc     *cwl.Document
		wantErr bool
	}{
		{
			name: "valid docker requirement",
			doc: &cwl.Document{
				Requirements: []cwl.Requirement{
					{
						Class:      "DockerRequirement",
						DockerPull: "biocontainers/bwa:0.7.17",
					},
				},
			},
			wantErr: false,
		},
		{
			name: "valid apptainer requirement",
			doc: &cwl.Document{
				Requirements: []cwl.Requirement{
					{
						Class:         "ApptainerRequirement",
						ApptainerPull: "library://sylabs/examples/lolcow:latest",
					},
				},
			},
			wantErr: false,
		},
		{
			name: "docker requirement in hints",
			doc: &cwl.Document{
				Hints: []cwl.Requirement{
					{
						Class:      "DockerRequirement",
						DockerPull: "python:3.11",
					},
				},
			},
			wantErr: false,
		},
		{
			name:    "no container requirement",
			doc:     &cwl.Document{},
			wantErr: true,
		},
		{
			name: "empty docker requirement",
			doc: &cwl.Document{
				Requirements: []cwl.Requirement{
					{Class: "DockerRequirement"},
				},
			},
			wantErr: true,
		},
		{
			name: "empty apptainer requirement",
			doc: &cwl.Document{
				Requirements: []cwl.Requirement{
					{Class: "ApptainerRequirement"},
				},
			},
			wantErr: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateContainerRequirement(tc.doc)
			if tc.wantErr && err == nil {
				t.Error("Expected error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}
		})
	}
}

func TestValidateBaseCommand(t *testing.T) {
	testCases := []struct {
		name        string
		baseCommand interface{}
		wantErr     bool
	}{
		{
			name:        "simple command",
			baseCommand: "bwa",
			wantErr:     false,
		},
		{
			name:        "command with subcommand",
			baseCommand: []interface{}{"bwa", "mem"},
			wantErr:     false,
		},
		{
			name:        "relative path",
			baseCommand: "./bin/bwa",
			wantErr:     true,
		},
		{
			name:        "absolute path",
			baseCommand: "/usr/bin/bwa",
			wantErr:     true,
		},
		{
			name:        "path in array",
			baseCommand: []interface{}{"/opt/tools/bwa", "mem"},
			wantErr:     true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			doc := &cwl.Document{BaseCommand: tc.baseCommand}
			err := ValidateBaseCommand(doc)
			if tc.wantErr && err == nil {
				t.Error("Expected error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}
		})
	}
}

func TestSanitizeImageName(t *testing.T) {
	testCases := []struct {
		input    string
		expected string
	}{
		{"python:3.11", "python_3.11"},
		{"biocontainers/bwa:0.7.17", "biocontainers_bwa_0.7.17"},
		{"docker://python:latest", "python_latest"},
		{"library://user/collection/image:tag", "user_collection_image_tag"},
		{"ghcr.io/org/repo:v1.0.0", "ghcr.io_org_repo_v1.0.0"},
	}

	for _, tc := range testCases {
		result := sanitizeImageName(tc.input)
		if result != tc.expected {
			t.Errorf("sanitizeImageName(%q) = %q, expected %q", tc.input, result, tc.expected)
		}
	}
}

func TestGetContainerSpec(t *testing.T) {
	t.Run("docker to apptainer conversion", func(t *testing.T) {
		doc := &cwl.Document{
			Requirements: []cwl.Requirement{
				{
					Class:      "DockerRequirement",
					DockerPull: "biocontainers/bwa:0.7.17",
				},
			},
		}

		spec := doc.GetContainerSpec(cwl.RuntimeApptainer)

		if spec.Runtime != cwl.RuntimeApptainer {
			t.Errorf("Expected runtime apptainer, got %s", spec.Runtime)
		}
		if spec.Pull != "docker://biocontainers/bwa:0.7.17" {
			t.Errorf("Expected docker:// prefix, got %s", spec.Pull)
		}
	})

	t.Run("native apptainer preferred", func(t *testing.T) {
		doc := &cwl.Document{
			Requirements: []cwl.Requirement{
				{
					Class:      "DockerRequirement",
					DockerPull: "biocontainers/bwa:0.7.17",
				},
			},
			Hints: []cwl.Requirement{
				{
					Class:         "ApptainerRequirement",
					ApptainerFile: "/path/to/bwa.sif",
				},
			},
		}

		spec := doc.GetContainerSpec(cwl.RuntimeApptainer)

		if spec.Image != "/path/to/bwa.sif" {
			t.Errorf("Expected native Apptainer image, got %s", spec.Image)
		}
	})

	t.Run("cuda requirement detected", func(t *testing.T) {
		doc := &cwl.Document{
			Requirements: []cwl.Requirement{
				{
					Class:      "DockerRequirement",
					DockerPull: "nvidia/cuda:11.0-base",
				},
			},
			Hints: []cwl.Requirement{
				{
					Class:                 "cwltool:CUDARequirement",
					CUDAVersionMin:        "11.0",
					CUDADeviceCountMin:    2,
					CUDAComputeCapability: "7.0",
				},
			},
		}

		spec := doc.GetContainerSpec(cwl.RuntimeDocker)

		if !spec.NeedsGPU {
			t.Error("Expected NeedsGPU to be true")
		}
		if spec.GPUCount != 2 {
			t.Errorf("Expected 2 GPUs, got %d", spec.GPUCount)
		}
		if spec.CUDAMinVersion != "11.0" {
			t.Errorf("Expected CUDA 11.0, got %s", spec.CUDAMinVersion)
		}
	})

	t.Run("no container requirement", func(t *testing.T) {
		doc := &cwl.Document{}
		spec := doc.GetContainerSpec(cwl.RuntimeDocker)

		if spec.Runtime != cwl.RuntimeNone {
			t.Errorf("Expected runtime none, got %s", spec.Runtime)
		}
	})
}

func TestRequiresContainer(t *testing.T) {
	t.Run("with docker", func(t *testing.T) {
		doc := &cwl.Document{
			Requirements: []cwl.Requirement{
				{Class: "DockerRequirement", DockerPull: "python:3"},
			},
		}
		if !doc.RequiresContainer() {
			t.Error("Expected RequiresContainer to be true")
		}
	})

	t.Run("with apptainer", func(t *testing.T) {
		doc := &cwl.Document{
			Hints: []cwl.Requirement{
				{Class: "ApptainerRequirement", ApptainerPull: "library://test"},
			},
		}
		if !doc.RequiresContainer() {
			t.Error("Expected RequiresContainer to be true")
		}
	})

	t.Run("without container", func(t *testing.T) {
		doc := &cwl.Document{}
		if doc.RequiresContainer() {
			t.Error("Expected RequiresContainer to be false")
		}
	})
}

// Package oci parses the manifest formats the registry accepts.
package oci

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/p-arndt/doreg/internal/digest"
)

// Media types.
const (
	MediaTypeOCIManifest     = "application/vnd.oci.image.manifest.v1+json"
	MediaTypeOCIIndex        = "application/vnd.oci.image.index.v1+json"
	MediaTypeDockerManifest  = "application/vnd.docker.distribution.manifest.v2+json"
	MediaTypeDockerList      = "application/vnd.docker.distribution.manifest.list.v2+json"
	MediaTypeDockerSchema1   = "application/vnd.docker.distribution.manifest.v1+json"
	MediaTypeDockerSchema1JW = "application/vnd.docker.distribution.manifest.v1+prettyjws"
	MediaTypeOCIEmpty        = "application/vnd.oci.empty.v1+json"
)

// IsIndex reports whether the media type is a multi-platform index.
func IsIndex(mt string) bool { return mt == MediaTypeOCIIndex || mt == MediaTypeDockerList }

// Platform describes the target of an index entry.
type Platform struct {
	Architecture string   `json:"architecture"`
	OS           string   `json:"os"`
	Variant      string   `json:"variant,omitempty"`
	OSVersion    string   `json:"os.version,omitempty"`
	OSFeatures   []string `json:"os.features,omitempty"`
}

// Descriptor points at content.
type Descriptor struct {
	MediaType    string            `json:"mediaType"`
	Digest       string            `json:"digest"`
	Size         int64             `json:"size"`
	URLs         []string          `json:"urls,omitempty"`
	Annotations  map[string]string `json:"annotations,omitempty"`
	Platform     *Platform         `json:"platform,omitempty"`
	ArtifactType string            `json:"artifactType,omitempty"`
}

// Manifest is the union of image manifest and index fields.
type Manifest struct {
	SchemaVersion int               `json:"schemaVersion"`
	MediaType     string            `json:"mediaType,omitempty"`
	ArtifactType  string            `json:"artifactType,omitempty"`
	Config        *Descriptor       `json:"config,omitempty"`
	Layers        []Descriptor      `json:"layers,omitempty"`
	Manifests     []Descriptor      `json:"manifests,omitempty"`
	Subject       *Descriptor       `json:"subject,omitempty"`
	Annotations   map[string]string `json:"annotations,omitempty"`
}

// ErrInvalid marks manifests the registry rejects.
var ErrInvalid = errors.New("invalid manifest")

// Parse decodes a manifest and determines its media type. contentType is the
// request Content-Type, used when the body omits mediaType (allowed for OCI).
func Parse(data []byte, contentType string) (*Manifest, error) {
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	if m.MediaType == "" {
		switch {
		case contentType == MediaTypeOCIManifest || contentType == MediaTypeOCIIndex ||
			contentType == MediaTypeDockerManifest || contentType == MediaTypeDockerList:
			m.MediaType = contentType
		case m.Manifests != nil:
			m.MediaType = MediaTypeOCIIndex
		default:
			m.MediaType = MediaTypeOCIManifest
		}
	}
	switch m.MediaType {
	case MediaTypeOCIManifest, MediaTypeDockerManifest:
		if m.Config == nil {
			return nil, fmt.Errorf("%w: missing config", ErrInvalid)
		}
		if err := checkDescriptor(*m.Config); err != nil {
			return nil, err
		}
		for _, l := range m.Layers {
			if err := checkDescriptor(l); err != nil {
				return nil, err
			}
		}
	case MediaTypeOCIIndex, MediaTypeDockerList:
		for _, c := range m.Manifests {
			if err := checkDescriptor(c); err != nil {
				return nil, err
			}
		}
	case MediaTypeDockerSchema1, MediaTypeDockerSchema1JW:
		return nil, fmt.Errorf("%w: schema1 manifests are not supported", ErrInvalid)
	default:
		return nil, fmt.Errorf("%w: unsupported media type %q", ErrInvalid, m.MediaType)
	}
	if m.SchemaVersion != 2 {
		return nil, fmt.Errorf("%w: schemaVersion must be 2", ErrInvalid)
	}
	if m.Subject != nil {
		if err := checkDescriptor(*m.Subject); err != nil {
			return nil, err
		}
	}
	return &m, nil
}

func checkDescriptor(d Descriptor) error {
	if _, err := digest.Parse(d.Digest); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	if d.Size < 0 {
		return fmt.Errorf("%w: negative size", ErrInvalid)
	}
	return nil
}

// EffectiveArtifactType is what the referrers API reports for a manifest.
func (m *Manifest) EffectiveArtifactType() string {
	if m.ArtifactType != "" {
		return m.ArtifactType
	}
	if m.Config != nil && !IsIndex(m.MediaType) {
		return m.Config.MediaType
	}
	return ""
}

// IsAttestation reports whether an index entry is a buildx attestation rather
// than a runnable platform image.
func IsAttestation(d Descriptor) bool {
	if d.Annotations["vnd.docker.reference.type"] == "attestation-manifest" {
		return true
	}
	return d.Platform != nil && d.Platform.OS == "unknown" && d.Platform.Architecture == "unknown"
}

// ImageConfig is the subset of the image config the UI shows.
type ImageConfig struct {
	Architecture string `json:"architecture"`
	OS           string `json:"os"`
	Variant      string `json:"variant,omitempty"`
	Created      string `json:"created,omitempty"`
	Author       string `json:"author,omitempty"`
	Config       struct {
		User         string              `json:"User,omitempty"`
		Env          []string            `json:"Env,omitempty"`
		Entrypoint   []string            `json:"Entrypoint,omitempty"`
		Cmd          []string            `json:"Cmd,omitempty"`
		WorkingDir   string              `json:"WorkingDir,omitempty"`
		Labels       map[string]string   `json:"Labels,omitempty"`
		ExposedPorts map[string]struct{} `json:"ExposedPorts,omitempty"`
		Volumes      map[string]struct{} `json:"Volumes,omitempty"`
		StopSignal   string              `json:"StopSignal,omitempty"`
	} `json:"config"`
	History []struct {
		Created    string `json:"created,omitempty"`
		CreatedBy  string `json:"created_by,omitempty"`
		Comment    string `json:"comment,omitempty"`
		EmptyLayer bool   `json:"empty_layer,omitempty"`
	} `json:"history,omitempty"`
}

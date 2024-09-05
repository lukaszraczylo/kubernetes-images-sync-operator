package shared

import (
	"regexp"
	"strings"
)

var BACKUP_JOB_IMAGE = "ghcr.io/lukaszraczylo/kubernetes-images-sync-worker:1.0.2"

const (
	// AVAILABLE STATUSES
	STATUS_PENDING  = "PENDING"
	STATUS_STARTING = "STARTING"
	STATUS_RETRYING = "RETRYING"
	STATUS_RUNNING  = "RUNNING"
	STATUS_FAILED   = "FAILED"
	STATUS_SUCCESS  = "COMPLETED"
	STATUS_PRESENT  = "PRESENT"

	// STORAGE DEFINITIONS
	STORAGE_S3   = "S3"
	STORAGE_FILE = "FILE"
)

type Container struct {
	Image          string `json:"image"`
	Tag            string `json:"tag"`
	Sha            string `json:"sha"`
	FullName       string `json:"fullName"`
	ImageNamespace string `json:"imageNamespace"`
}

type ContainersList struct {
	Containers []Container `json:"containers"`
}

func RemoveDuplicates(containersList ContainersList) ContainersList {
	// remove duplicates from the list
	encountered := map[Container]bool{}
	result := ContainersList{}
	for v := range containersList.Containers {
		if !encountered[containersList.Containers[v]] {
			encountered[containersList.Containers[v]] = true
			result.Containers = append(result.Containers, containersList.Containers[v])
		}
	}
	return result
}

func RemoveExcludedImages(containers ContainersList, excludes []string) ContainersList {
	// remove excluded images from the list
	result := ContainersList{}
	for _, container := range containers.Containers {
		excluded := false
		for _, exclude := range excludes {
			if strings.Contains(strings.ToLower(container.Image), strings.ToLower(exclude)) {
				excluded = true
				break
			}
		}
		if !excluded {
			result.Containers = append(result.Containers, container)
		}
	}
	return result
}

func IncludeOnlyImages(containers ContainersList, includes []string) ContainersList {
	// include only images from the list
	result := ContainersList{}
	for _, container := range containers.Containers {
		included := false
		for _, include := range includes {
			if strings.Contains(strings.ToLower(container.Image), strings.ToLower(include)) {
				included = true
				break
			}
		}
		if included {
			result.Containers = append(result.Containers, container)
		}
	}
	return result
}

var imageNameRegexp = regexp.MustCompile(`[/:@&=+$,\?%\{\}\[\]\\^~#\s]`)
var imageNameRegexpReplace = regexp.MustCompile(`-+`)

func NormalizeImageName(name string) string {
	// Replace special characters with hyphens
	normalized := imageNameRegexp.ReplaceAllString(name, "-")

	// Remove consecutive hyphens
	normalized = imageNameRegexpReplace.ReplaceAllString(normalized, "-")

	// Trim leading and trailing hyphens
	return strings.Trim(normalized, "-")
}

// filterOnlyFromNamespaces filters out containers from namespaces that are not in the list
func FilterOnlyFromNamespaces(containers ContainersList, namespaces []string) ContainersList {
	result := ContainersList{}
	for _, container := range containers.Containers {
		for _, namespace := range namespaces {
			if container.ImageNamespace == namespace {
				result.Containers = append(result.Containers, container)
			}
		}
	}
	return result
}

// filterOutWholeNamespaces filters out containers from namespaces that are in the list
func FilterOutWholeNamespaces(containers ContainersList, namespaces []string) ContainersList {
	result := ContainersList{}
	for _, container := range containers.Containers {
		excluded := false
		for _, namespace := range namespaces {
			if container.ImageNamespace == namespace {
				excluded = true
				break
			}
		}
		if !excluded {
			result.Containers = append(result.Containers, container)
		}
	}
	return result
}

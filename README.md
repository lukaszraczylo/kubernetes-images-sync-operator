# kubernetes-images-sync-operator

Kubernetes operator backing up images into the local / S3 compatible storage.

## Description

Operator was created to simplify the impex between open-to-world and air-gapped environment.
As transfer of the deployment manifests is relatively easy, images are a completely different story.
Air-gapped environments usually have issues with missing images and amount of data required to be transferred between them.
This operator takes care of it and ensures that no images were missed out ( including initImages and ephemeralImages ) and 
impex itself is as small as possible due to the cross comparison with previouslly executed backups.

## Getting Started

Operator installation


```
helm repo add raczylo https://lukaszraczylo.github.io/helm-charts/
helm install raczylo/kube-images-sync
```

## Starting the backup

Please remember that backups are triggered whenever the new object appears

```yaml
apiVersion: raczylo.com/v1
kind: ClusterImageExport
metadata:
  name: backup-20240901
spec:
  name: backup-20240901
  jobAnnotations:
    my-fancy-export: 11-09-2024
  # Excludes will remove all images with listed wording from the backup list
  # excludes:
  #   - nginx

  # Includes will add ONLY images with listed wording to the backup list
  includes:
    - busybox

  # Works only with images within specified namespaces
  # namespaces:
  #  - default
  #  - longhorn

  # Works with all images EXCEPT of the ones within namespaces specified
  # excludedNamespaces:
  #  - my-awesome-namespace

  additionalImages:
    - minio/minio:RELEASE.2024-09-09T16-59-28Z

  basePath: /images # base path in the target directory
  storage:
    target: S3 # file backup is not ready yet
    s3:
      bucket: my-backup-in-s3
      region: us-west-2
      accessKey: yyy
      secretKey: zzz
    # Endpoint allows you to direct the backup to your own S3 compatible endpoint like minio
    # endpoint: http://127.0.0.1:8010
    # secretName: my-secret-in-cluster # Not ready yet
    # useRole: true # Current role to be used instead of access / secret keys
    # roleARN: my-awesome-role # Instead of picking the default role, use the specified one
  maxConcurrentJobs: 1
```

## Automatic Cleanup (TTL & Retention)

To prevent old exports from accumulating, you can configure automatic cleanup using TTL (time-based) or retention policies (count-based).

> **WARNING**: When a ClusterImageExport is deleted, the actual backed up images in storage are also deleted. Make sure your retention settings align with your backup requirements.

### TTL-based cleanup

Delete exports after a specified number of days:

```yaml
apiVersion: raczylo.com/v1
kind: ClusterImageExport
metadata:
  name: daily-backup-2024-12-18
spec:
  name: daily-backup
  basePath: /backups/daily
  storage:
    target: S3
    s3:
      bucket: my-backup-bucket
      region: eu-west-1
      useRole: true
  maxConcurrentJobs: 5
  # Delete this backup 30 days after completion
  ttlDaysAfterFinished: 30
```

### Retention-based cleanup

Keep only the last N successful/failed exports per base path:

```yaml
apiVersion: raczylo.com/v1
kind: ClusterImageExport
metadata:
  name: weekly-backup-2024-w51
spec:
  name: weekly-backup
  basePath: /backups/weekly
  storage:
    target: S3
    s3:
      bucket: my-backup-bucket
      region: eu-west-1
      useRole: true
  maxConcurrentJobs: 5
  # Keep the last 12 successful backups (3 months of weekly backups)
  # Keep only the last 2 failed backups for debugging
  retention:
    maxSuccessful: 12
    maxFailed: 2
```

### Combined TTL + Retention

You can use both policies together. The export will be deleted when either condition is met:

```yaml
apiVersion: raczylo.com/v1
kind: ClusterImageExport
metadata:
  name: monthly-backup-2024-12
spec:
  name: monthly-backup
  basePath: /backups/monthly
  storage:
    target: S3
    s3:
      bucket: my-backup-bucket
      region: eu-west-1
      useRole: true
  maxConcurrentJobs: 10
  # Keep backups for up to 1 year
  ttlDaysAfterFinished: 365
  # But also limit to last 12 monthly backups
  retention:
    maxSuccessful: 12
    maxFailed: 1
```

## Worth knowing

* If you provide roleARN, you also need to set the useRole to true.

#### Random fluff

Copyright 2024.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.


#!/bin/bash
set -e

PODMAN_AUTH_FILE="/home/runner/.config/containers/auth.json"

# Initialize the auth file if it doesn't exist or is empty
mkdir -p $(dirname $PODMAN_AUTH_FILE)
if [ ! -s "$PODMAN_AUTH_FILE" ]; then
    echo '{"auths":{}}' > $PODMAN_AUTH_FILE
fi

# Loop through all mounted secret directories
for secret_dir in /home/runner/.docker-secret-*; do
    if [ -d "$secret_dir" ]; then
        config_file="$secret_dir/.dockerconfigjson"
        if [ -f "$config_file" ]; then
            # Merge the auth data into the podman auth file
            jq -s '.[0].auths * .[1].auths | {auths: .}' $PODMAN_AUTH_FILE $config_file > ${PODMAN_AUTH_FILE}.tmp
            mv ${PODMAN_AUTH_FILE}.tmp $PODMAN_AUTH_FILE
            # Extract registry, username, and password from the config file
            jq -r '.auths | to_entries[] | "\(.key) \(.value.auth)"' $config_file | while read registry auth; do
                username=$(echo $auth | base64 -d | cut -d: -f1)
                password=$(echo $auth | base64 -d | cut -d: -f2-)
                # Perform podman login
                podman login --username "$username" --password "$password" "$registry"
                echo "podman: Successfully logged into $registry"
            done
        fi
    fi
done

# Run the original command
exec "$@"
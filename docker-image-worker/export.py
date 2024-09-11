#!/usr/bin/env python3
import os
import sys
import argparse
from botocore.exceptions import ClientError

sys.path.append(os.path.dirname(os.path.abspath(__file__)))
from s3_utils import get_s3_client, parse_s3_path, add_common_arguments, validate_args

def transfer_file(source, destination, use_role=False, role_name=None, aws_access_key_id=None, aws_secret_access_key=None, endpoint_url=None, region=None):
    """
    Transfer a file from a local source to either a local destination or an S3 bucket
    """
    if not os.path.isfile(source):
        print(f"Error: Source file '{source}' does not exist or is not a file.")
        return False

    if destination.startswith('s3://'):
        # Uploading to S3
        s3_client = get_s3_client(use_role, role_name, aws_access_key_id, aws_secret_access_key, endpoint_url, region)
        bucket, s3_key = parse_s3_path(destination)
        try:
            s3_client.upload_file(source, bucket, s3_key)
            print(f"File {source} uploaded successfully to {destination}")
        except ClientError as e:
            print(f"Error uploading file: {str(e)}")
            return False
    else:
        # Copying to local destination
        try:
            import shutil
            # Create destination directory if it doesn't exist
            os.makedirs(os.path.dirname(destination), exist_ok=True)
            shutil.copy2(source, destination)
            print(f"File {source} copied successfully to {destination}")
        except IOError as e:
            print(f"Error copying file: {str(e)}")
            return False
    return True

if __name__ == "__main__":
    parser = argparse.ArgumentParser(description="Transfer a file from a local source to either a local destination or an S3 bucket.")
    parser.add_argument("source", help="The local source file path")
    parser.add_argument("destination", help="The destination file path (local) or S3 path (e.g., 's3://bucket/key')")
    add_common_arguments(parser)

    args = parser.parse_args()
    validate_args(args, parser)

    success = transfer_file(
        args.source,
        args.destination,
        args.use_role,
        args.role_name,
        args.aws_access_key_id,
        args.aws_secret_access_key,
        args.endpoint_url,
        args.region
    )

    if success:
        print("Transfer completed successfully.")
    else:
        print("Transfer failed.")
        exit(1)
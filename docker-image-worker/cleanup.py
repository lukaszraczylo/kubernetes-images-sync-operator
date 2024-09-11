#!/usr/bin/env python3
import os
import sys
import argparse
from botocore.exceptions import ClientError

sys.path.append(os.path.dirname(os.path.abspath(__file__)))

from s3_utils import get_s3_client, parse_s3_path, add_common_arguments, validate_args

def remove_directory(destination, use_role=False, role_name=None, aws_access_key_id=None, aws_secret_access_key=None, endpoint_url=None, region=None):
    """
    Remove a directory recursively, either local or in an S3 bucket
    """
    if destination.startswith('s3://'):
        # Removing from S3
        s3_client = get_s3_client(use_role, role_name, aws_access_key_id, aws_secret_access_key, endpoint_url, region)
        bucket, prefix = parse_s3_path(destination)
        try:
            paginator = s3_client.get_paginator('list_objects_v2')
            for page in paginator.paginate(Bucket=bucket, Prefix=prefix):
                if 'Contents' in page:
                    objects_to_delete = [{'Key': obj['Key']} for obj in page['Contents']]
                    s3_client.delete_objects(Bucket=bucket, Delete={'Objects': objects_to_delete})
            print(f"Directory {destination} removed successfully from S3")
        except ClientError as e:
            print(f"Error removing directory from S3: {str(e)}")
            return False
    else:
        # Removing local directory
        try:
            import shutil
            if os.path.exists(destination):
                shutil.rmtree(destination)
                print(f"Directory {destination} removed successfully")
            else:
                print(f"Directory {destination} does not exist")
        except IOError as e:
            print(f"Error removing directory: {str(e)}")
            return False
    return True

if __name__ == "__main__":
    parser = argparse.ArgumentParser(description="Remove a directory recursively, either local or in an S3 bucket.")
    parser.add_argument("destination", help="The directory path (local) or S3 path (e.g., 's3://bucket/prefix') to remove")
    add_common_arguments(parser)

    args = parser.parse_args()
    validate_args(args, parser)

    success = remove_directory(
        args.destination,
        args.use_role,
        args.role_name,
        args.aws_access_key_id,
        args.aws_secret_access_key,
        args.endpoint_url,
        args.region
    )

    if success:
        print("Cleanup completed successfully.")
    else:
        print("Cleanup failed.")
        exit(1)
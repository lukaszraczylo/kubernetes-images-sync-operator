#!/usr/bin/env python3
import os
import boto3
import argparse
from botocore.exceptions import ClientError

def get_s3_client(use_role=False, role_name=None, aws_access_key_id=None, aws_secret_access_key=None, endpoint_url=None, region=None):
    """
    Create and return an S3 client based on the provided authentication method, endpoint, and region.
    """
    client_kwargs = {}
    
    if endpoint_url:
        client_kwargs['endpoint_url'] = endpoint_url
    elif region:
        client_kwargs['region_name'] = region

    if use_role:
        if role_name:
            # Assume the specified role
            sts_client = boto3.client('sts')
            assumed_role_object = sts_client.assume_role(
                RoleArn=f"arn:aws:iam::{boto3.client('sts').get_caller_identity()['Account']}:role/{role_name}",
                RoleSessionName="AssumeRoleSession"
            )
            credentials = assumed_role_object['Credentials']
            client_kwargs['aws_access_key_id'] = credentials['AccessKeyId']
            client_kwargs['aws_secret_access_key'] = credentials['SecretAccessKey']
            client_kwargs['aws_session_token'] = credentials['SessionToken']
        return boto3.client('s3', **client_kwargs)
    elif aws_access_key_id and aws_secret_access_key:
        client_kwargs['aws_access_key_id'] = aws_access_key_id
        client_kwargs['aws_secret_access_key'] = aws_secret_access_key
        return boto3.client('s3', **client_kwargs)
    else:
        raise ValueError("Either use_role must be True, or both aws_access_key_id and aws_secret_access_key must be provided")

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

def parse_s3_path(s3_path):
    """
    Parse an S3 path into bucket and key
    """
    parts = s3_path.replace('s3://', '').split('/', 1)
    bucket = parts[0]
    key = parts[1] if len(parts) > 1 else ''
    return bucket, key

if __name__ == "__main__":
    parser = argparse.ArgumentParser(description="Remove a directory recursively, either local or in an S3 bucket.")
    parser.add_argument("destination", help="The directory path (local) or S3 path (e.g., 's3://bucket/prefix') to remove")
    parser.add_argument("--use_role", action="store_true", help="Use IAM role for authentication")
    parser.add_argument("--role_name", help="The name of the IAM role to assume")
    parser.add_argument("--aws_access_key_id", help="AWS access key ID")
    parser.add_argument("--aws_secret_access_key", help="AWS secret access key")
    parser.add_argument("--endpoint_url", help="S3-compatible endpoint URL")
    parser.add_argument("--region", help="AWS region (ignored if endpoint_url is specified)")

    args = parser.parse_args()

    if args.destination.startswith('s3://'):
        if args.use_role and (args.aws_access_key_id or args.aws_secret_access_key or args.endpoint_url):
            parser.error("When using IAM role (--use_role), access key, secret, and endpoint URL should not be specified.")

        if (args.aws_access_key_id or args.aws_secret_access_key) and not (args.aws_access_key_id and args.aws_secret_access_key):
            parser.error("Both --aws_access_key_id and --aws_secret_access_key must be provided when using access key authentication.")

        if not args.use_role and not (args.aws_access_key_id and args.aws_secret_access_key):
            parser.error("Either --use_role or both --aws_access_key_id and --aws_secret_access_key must be provided for S3 operations.")

        if args.use_role and args.role_name and (args.aws_access_key_id or args.aws_secret_access_key):
            parser.error("When using a specific role (--role_name), access key and secret should not be specified.")

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
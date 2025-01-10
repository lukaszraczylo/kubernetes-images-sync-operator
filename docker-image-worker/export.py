#!/usr/bin/env python3
import os
import sys
import argparse
import logging
from botocore.exceptions import ClientError, BotoCoreError
from tenacity import retry, stop_after_attempt, wait_fixed

# Configure logging
logging.basicConfig(
    level=logging.INFO,
    format='%(asctime)s - %(name)s - %(levelname)s - %(message)s'
)
logger = logging.getLogger(__name__)

sys.path.append(os.path.dirname(os.path.abspath(__file__)))
from s3_utils import get_s3_client, parse_s3_path, add_common_arguments, validate_args

def log_error_details(e):
    """Log detailed error information from AWS exceptions"""
    if hasattr(e, 'response'):
        error_code = e.response.get('Error', {}).get('Code', 'Unknown')
        error_message = e.response.get('Error', {}).get('Message', str(e))
        request_id = e.response.get('ResponseMetadata', {}).get('RequestId', 'Unknown')
        logger.error(f"AWS Error Details:")
        logger.error(f"- Error Code: {error_code}")
        logger.error(f"- Error Message: {error_message}")
        logger.error(f"- Request ID: {request_id}")
        logger.error(f"- Full Response: {e.response}")
    else:
        logger.error(f"Non-AWS Error: {str(e)}")

@retry(stop=stop_after_attempt(5), wait=wait_fixed(5))
def transfer_file(source, destination, use_role=False, role_name=None, use_current_role=False, aws_access_key_id=None, aws_secret_access_key=None, endpoint_url=None, region=None):
    """
    Transfer a file from a local source to either a local destination or an S3 bucket
    """
    if not os.path.isfile(source):
        logger.error(f"Error: Source file '{source}' does not exist or is not a file.")
        return False

    if destination.startswith('s3://'):
        # Uploading to S3
        try:
            logger.info(f"Attempting to upload {source} to {destination}")
            s3_client = get_s3_client(use_role, role_name, use_current_role, aws_access_key_id, aws_secret_access_key, endpoint_url, region)
            bucket, s3_key = parse_s3_path(destination)
            
            try:
                s3_client.upload_file(source, bucket, s3_key)
                logger.info(f"File {source} uploaded successfully to {destination}")
            except ClientError as e:
                log_error_details(e)
                if "AccessDenied" in str(e):
                    logger.error("Access denied. Please check:")
                    logger.error("1. IAM role/user permissions")
                    logger.error("2. S3 bucket permissions")
                    logger.error("3. Web identity token configuration")
                return False
            except BotoCoreError as e:
                logger.error(f"Boto3 error during upload: {str(e)}")
                return False
            
        except Exception as e:
            logger.error(f"Unexpected error during S3 client creation or upload: {str(e)}")
            return False
    else:
        # Copying to local destination
        try:
            import shutil
            logger.info(f"Attempting to copy {source} to local destination {destination}")
            # Create destination directory if it doesn't exist
            os.makedirs(os.path.dirname(destination), exist_ok=True)
            shutil.copy2(source, destination)
            logger.info(f"File {source} copied successfully to {destination}")
        except IOError as e:
            logger.error(f"Error copying file: {str(e)}")
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
        args.use_current_role,
        args.aws_access_key_id,
        args.aws_secret_access_key,
        args.endpoint_url,
        args.region
    )

    if success:
        logger.info("Transfer completed successfully.")
    else:
        logger.error("Transfer failed.")
        exit(1)

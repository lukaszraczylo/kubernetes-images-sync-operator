import boto3
from botocore.exceptions import ClientError

import os
import logging

def get_s3_client(use_role=False, role_name=None, use_current_role=False, aws_access_key_id=None, aws_secret_access_key=None, endpoint_url=None, region=None):
    """
    Create and return an S3 client based on the provided authentication method, endpoint, and region.
    """
    logging.basicConfig(level=logging.INFO)
    logger = logging.getLogger(__name__)

    client_kwargs = {}
    
    # Log authentication method being attempted
    logger.info("Attempting S3 client creation with:")
    logger.info(f"- Region: {region if region else 'default'}")
    logger.info(f"- Endpoint URL: {endpoint_url if endpoint_url else 'default'}")
    
    if endpoint_url:
        client_kwargs['endpoint_url'] = endpoint_url
    if region:
        client_kwargs['region_name'] = region

    # Check for AWS Web Identity token
    token_file = os.environ.get('AWS_WEB_IDENTITY_TOKEN_FILE')
    role_arn = os.environ.get('AWS_ROLE_ARN')
    if token_file or role_arn:
        logger.info("AWS Web Identity configuration detected:")
        logger.info(f"- Token file path: {token_file}")
        logger.info(f"- Role ARN: {role_arn}")
        logger.info(f"- Session name: {os.environ.get('AWS_ROLE_SESSION_NAME', 'default')}")

    if aws_access_key_id and aws_secret_access_key:
        logger.info("Using explicit AWS credentials")
        # Use explicit credentials if provided
        client_kwargs['aws_access_key_id'] = aws_access_key_id
        client_kwargs['aws_secret_access_key'] = aws_secret_access_key
        return boto3.client('s3', **client_kwargs)
    elif use_role and role_name:
        # Assume specific role if requested
        logger.info(f"Attempting to assume role: {role_name}")
        try:
            sts_client = boto3.client('sts')
            # Get current identity for logging
            identity = sts_client.get_caller_identity()
            logger.info(f"Current identity: {identity['Arn']}")
            
            assumed_role_object = sts_client.assume_role(
                RoleArn=f"arn:aws:iam::{boto3.client('sts').get_caller_identity()['Account']}:role/{role_name}",
                RoleSessionName="AssumeRoleSession"
            )
            credentials = assumed_role_object['Credentials']
            client_kwargs['aws_access_key_id'] = credentials['AccessKeyId']
            client_kwargs['aws_secret_access_key'] = credentials['SecretAccessKey']
            client_kwargs['aws_session_token'] = credentials['SessionToken']
            return boto3.client('s3', **client_kwargs)
        except Exception as e:
            logger.error(f"Failed to assume role {role_name}: {str(e)}")
            raise
    elif use_current_role:
        # Use the current role (e.g., from Kubernetes service account)
        logger.info("Using current role from environment")
        try:
            # Log environment for debugging
            for key, value in sorted(os.environ.items()):
                if any(k in key.lower() for k in ['aws', 'role', 'auth', 'token', 'credential']):
                    logger.info(f"Environment: {key}={value}")

            # Get the AWS region from environment or parameter
            aws_region = os.environ.get('AWS_REGION') or os.environ.get('AWS_DEFAULT_REGION')
            if not aws_region and not region:
                raise ValueError("AWS region must be specified either through region parameter or AWS_REGION environment variable")

            # Use region from parameter only if not set in environment
            if not aws_region:
                aws_region = region
                # Set it in environment for other AWS clients
                os.environ['AWS_REGION'] = region

            logger.info(f"Using AWS region: {aws_region}")

            # Create an STS client in the correct region
            sts_kwargs = {'endpoint_url': f'https://sts.{aws_region}.amazonaws.com'}
            if not os.environ.get('AWS_REGION') and not os.environ.get('AWS_DEFAULT_REGION'):
                sts_kwargs['region_name'] = aws_region
            sts = boto3.client('sts', **sts_kwargs)

            # Read the web identity token
            token_file = os.environ.get('AWS_WEB_IDENTITY_TOKEN_FILE')
            role_arn = os.environ.get('AWS_ROLE_ARN')

            if not token_file or not role_arn:
                raise ValueError("AWS_WEB_IDENTITY_TOKEN_FILE and AWS_ROLE_ARN must be set")

            with open(token_file, 'r') as f:
                token = f.read().strip()

            logger.info("Successfully read web identity token")
            logger.info(f"Using role ARN: {role_arn}")

            # Assume role with web identity using regional endpoint
            try:
                response = sts.assume_role_with_web_identity(
                    RoleArn=role_arn,
                    RoleSessionName=os.environ.get('AWS_ROLE_SESSION_NAME', 'WebIdentitySession'),
                    WebIdentityToken=token
                )

                # Get the temporary credentials
                credentials = response['Credentials']
                
                # Create the S3 client with the temporary credentials
                s3_kwargs = {
                    'aws_access_key_id': credentials['AccessKeyId'],
                    'aws_secret_access_key': credentials['SecretAccessKey'],
                    'aws_session_token': credentials['SessionToken']
                }
                # Only set region_name if not already in environment
                if not os.environ.get('AWS_REGION') and not os.environ.get('AWS_DEFAULT_REGION'):
                    s3_kwargs['region_name'] = aws_region
                # Add any additional kwargs
                s3_kwargs.update(client_kwargs)
                client = boto3.client('s3', **s3_kwargs)

                logger.info(f"Successfully assumed role with web identity: {response['AssumedRoleUser']['Arn']}")
                
                # Test the credentials
                try:
                    # Try to get caller identity first
                    sts_test = boto3.client(
                        'sts',
                        region_name=aws_region,
                        aws_access_key_id=credentials['AccessKeyId'],
                        aws_secret_access_key=credentials['SecretAccessKey'],
                        aws_session_token=credentials['SessionToken']
                    )
                    identity = sts_test.get_caller_identity()
                    logger.info(f"Successfully verified credentials as: {identity['Arn']}")
                    
                    # Then try S3 access
                    bucket_name = os.environ.get('BUCKET_NAME', 'default-bucket')
                    try:
                        client.head_bucket(Bucket=bucket_name)
                        logger.info(f"Successfully verified S3 access to bucket: {bucket_name}")
                    except ClientError as e:
                        error_code = e.response['Error']['Code']
                        if error_code == '404':
                            logger.warning(f"Bucket {bucket_name} does not exist, but credentials work")
                        else:
                            logger.warning(f"S3 access check failed: {error_code} - {e.response['Error']['Message']}")
                except Exception as e:
                    logger.warning(f"Could not verify credentials: {str(e)}")
                
                return client

            except ClientError as e:
                error_code = e.response['Error']['Code']
                error_message = e.response['Error']['Message']
                logger.error("Failed to assume role with web identity:")
                logger.error(f"Error Code: {error_code}")
                logger.error(f"Error Message: {error_message}")
                logger.error("Trust policy might need to be updated to allow sts:AssumeRoleWithWebIdentity")
                logger.error("Current role ARN: " + role_arn)
                logger.error("Token file path: " + token_file)
                raise
        except Exception as e:
            logger.error(f"Failed to use current role: {str(e)}")
            logger.error("Current environment:")
            for key, value in sorted(os.environ.items()):
                if any(k in key.lower() for k in ['aws', 'role', 'auth', 'token', 'credential']):
                    logger.error(f"  {key}: {value}")
            raise
    else:
        # Use default credentials (environment, instance profile, or pod service account)
        logger.info("Using default credential provider chain")
        try:
            client = boto3.client('s3', **client_kwargs)
            # Try to get caller identity to verify credentials
            sts = boto3.client('sts')
            identity = sts.get_caller_identity()
            logger.info(f"Successfully authenticated as: {identity['Arn']}")
            return client
        except Exception as e:
            logger.error(f"Failed to create S3 client: {str(e)}")
            raise

def parse_s3_path(s3_path):
    """
    Parse an S3 path into bucket and key
    """
    parts = s3_path.replace('s3://', '').split('/', 1)
    bucket = parts[0]
    key = parts[1] if len(parts) > 1 else ''
    return bucket, key

def add_common_arguments(parser):
    """
    Add common command-line arguments to an ArgumentParser object
    """
    auth_group = parser.add_mutually_exclusive_group()
    auth_group.add_argument("--use_role", action="store_true", help="Use IAM role for authentication")
    auth_group.add_argument("--use_current_role", action="store_true", help="Use current AWS role (e.g. from Kubernetes service account)")
    parser.add_argument("--role_name", help="The name of the IAM role to assume (only when --use_role is set)")
    parser.add_argument("--aws_access_key_id", help="AWS access key ID")
    parser.add_argument("--aws_secret_access_key", help="AWS secret access key")
    parser.add_argument("--endpoint_url", help="S3-compatible endpoint URL")
    parser.add_argument("--region", help="AWS region (ignored if endpoint_url is specified)")

def validate_args(args, parser):
    """
    Validate command-line arguments
    """
    if args.destination.startswith('s3://'):
        # Check for conflicting auth methods
        if args.use_role and not args.role_name:
            parser.error("--role_name is required when using --use_role")
            
        if args.role_name and not args.use_role:
            parser.error("--role_name can only be used with --use_role")

        if args.use_current_role and (args.aws_access_key_id or args.aws_secret_access_key):
            parser.error("When using current role (--use_current_role), access key and secret should not be specified")

        # If using explicit credentials, require both key and secret
        if (args.aws_access_key_id or args.aws_secret_access_key) and not (args.aws_access_key_id and args.aws_secret_access_key):
            parser.error("Both --aws_access_key_id and --aws_secret_access_key must be provided when using access key authentication")

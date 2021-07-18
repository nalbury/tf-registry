# tf-registry
Self Hosted Terraform Registry backed by S3

## Usage


`tf-registry` Provides a simple http server that implements the [Terraform Module Registry Protocol](https://www.terraform.io/docs/internals/module-registry-protocol.html).

```
Terraform Registry Server

Usage: tf-registry [flags] 

Flags:
  -bucket string
    	aws s3 bucket name containing terraform modules
  -port string
    	port for HTTP server (default "3000")
  -prefix string
    	optional path prefix for modules in s3
  -profile string
    	aws named profile to assume (default "default")
```

### Uploading Modules
`tf-registry` uses S3 as its backend storage, and at the moment it is read only, meaning that modules must be manually uploaded to s3 before they can be retrieved via `tf-registry`.

The path format must match the expected format: `s3://<bucket>/[optional_prefix]/<registry_namespace>/<module_name>/<provider>/<version>/<module_name>.tgz`

Example upload script (run from local module path):
```
#!/bin/bash

BUCKET="tf-registry-storage"
MODULE_NAME=$(basename $PWD)
REGISTY_NAMESPACE="nalbury"
PROVIDER="aws"
VERSION="1.0.0"

TMP_DIR=$(mktemp -d)
tar -czf ${TMP_DIR}/${MODULE_NAME}.tgz .
aws s3 cp ${TMP_DIR}/${MODULE_NAME}.tgz \
  s3://${BUCKET_NAME}/${REGISTRY_NAMESPACE}/{MODULE_NAME}/${PROVIDER}/${VERSION}/${MODULE_NAME}.tgz
rm -rf ${TMP_DIR}
```

### Using Modules from the Registry 
Once the module has been uploaded, and the server is running, you can then reference a module using the [standard registry source format](https://www.terraform.io/docs/language/modules/sources.html#terraform-registry):

```
module "eks_cluster" {
  source  = "tf-registry.mydomain.io/nalbury/my-aws-module/aws"
  version = "~> 1.0.0"
}
```
**NOTE** Terraform will only install modules if your registry is served over HTTPS. You can use [ngrok](https://ngrok.com) for a local server if necessary.

## TODO

Aside from any `TODO`s mentioned in the code, `tf-registry` should ideally have:
- [ ] Build CI + Release Artficats + Container Image
- [ ] Helm Chart for running `tf-registry`
- [ ] Terraform Module for running `tf-registry` (hosted publicly)
- [ ] Module upload support either via a custom client (wrap s3 api), or via the HTTP API directly
- [ ] Authentication
- [ ] Provider registry support
- [ ] Additional backend storage providers (gcp, azure, local FS)

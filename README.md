# parameter-secret-sync

A job to read parameters from AWS SSM Parameter Store and store them in Kubernetes as [Secrets](https://kubernetes.io/docs/concepts/configuration/secret/Secrets), designed to run as a Kubernetes [Job](https://kubernetes.io/docs/concepts/workloads/controllers/jobs-run-to-completion/), for one off execution, as oppsed to an ongoing controller.

Parameter values must be stored in json format. The json object is expected to represent a single level string map, where the map values are Base64 encoded strings: 
e.g `{"file1":"...Base64...", "file2":"...Base64..."}`

The generated secret will be named according to the last token of the standard slash 
delimted Parameter Name. e.g. `/dev/secrets/mysecret` results in a secret names `mysecret`

For running as a kubernetes job, see example template in [param-secret-sync-job.yaml](kubernetes/param-secret-sync-job.yaml)

The build system has been adopted from the awsome Tim Hockin and the Kubernetes community [https://github.com/thockin/go-build-template](https://github.com/thockin/go-build-template)


## Arguments
+  `-kubeconfig`  (or KUBECONFIG env var) kubeconfig file (needed for out of cluster execution)
+  `-namespace` 
    	target secret namespace (default "default")
+ `-params` 
    	comma separated list of param names
+  `-type` 
    	kubernetes secret type for (applies to the whole list of params) (default "Opaque")
 
## Building
see 
[https://github.com/thockin/go-build-template#building](https://github.com/thockin/go-build-template#building)
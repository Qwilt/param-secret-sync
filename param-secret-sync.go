package main

import (
	"flag"
	"log"
	"os"
	"strings"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ssm"
	"github.com/nirdothan/param-secret-sync/version"
	apicorev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

func getParamsFromAWS(ssmSvc *ssm.SSM, paramNames *[]*string) *ssm.GetParametersOutput {
	params := &ssm.GetParametersInput{
		Names:          *paramNames,
		WithDecryption: aws.Bool(true),
	}
	vals, err := ssmSvc.GetParameters(params)
	if err != nil {
		log.Printf("Failed to get parameters from AWS [%v]", err)
		os.Exit(1)
	}

	log.Println("Returned values from AWS:")
	for _, v := range vals.Parameters {
		log.Printf("  [%s]=>[%v]", *v.Name, *v.Value)
	}
	return vals
}

// left trim the string until the last slash char
func getSecretNameFromParam(name string) string {
	return name[strings.LastIndex(name, "/")+1:]
}

func crateSecret(client *kubernetes.Clientset, namespace string, param *ssm.Parameter) error {

	// if the param name includes a '_', split the string and use the first part as
	// secret name and the remainder as .data key
	// otherwise use the full name as both the secret name and .data key
	rawName := getSecretNameFromParam(*(param.Name))
	i := strings.Index(rawName, "_")
	var (
		name string
		key  string
	)

	// '_' not found
	if i == -1 {
		name, key = rawName, rawName
	} else {
		name = rawName[:i]
		key = rawName[i+1:]
	}

	if len(name) < 1 || len(key) < 1 {
		log.Printf("Illegal Parameter Value format [%s]", rawName)
		os.Exit(1)
	}

	secret := &apicorev1.Secret{
		ObjectMeta: meta_v1.ObjectMeta{
			Name: name,
			Labels: map[string]string{
				"heritage": "param-secret-sync",
			},
		},
		Type: "Opaque",
		StringData: map[string]string{
			key: *param.Value,
		},
	}
	log.Printf("creating secret [%s] in namespace [%s]", name, namespace)
	_, err := client.CoreV1().Secrets(namespace).Create(secret)
	if err != nil {
		if errors.IsAlreadyExists(err) {
			log.Printf("Secret [%s] already exists. Deleteting and recreating", name)
			err = client.CoreV1().Secrets(namespace).Delete(name, &meta_v1.DeleteOptions{})
			if err != nil {
				log.Fatal(err.Error())
			}
			_, err := client.CoreV1().Secrets(namespace).Create(secret)
			if err != nil {
				log.Fatal(err.Error())
			}
			return nil
		}

		log.Printf("Failed to create secret [%s] in kubernetes", name)

		return err
	}
	return nil

}
func copyParamPtrs(s *[]string) *[]*string {
	t := make([]*string, len(*s))
	for i := 0; i < len(*s); i++ {
		t[i] = &((*s)[i])
	}
	return &t
}

func main() {

	log.Printf("param-secret-sync version %s", version.VERSION)
	var (
		config *rest.Config
		err    error
	)

	paramList, kubeconfig, namespace := "", "", "default"
	flag.StringVar(&paramList, "params", paramList, "comma separated list of param names")
	flag.StringVar(&kubeconfig, "kubeconfig", kubeconfig, "kubeconfig file")
	flag.StringVar(&namespace, "namespace", namespace, "target secret namespace")
	flag.Parse()

	if paramList == "" {
		log.Fatal("No param names provided!")
	}

	ssmParams := strings.Split(paramList, ",")

	// Parse kubeconfig flag, KUBECONFIG env var or inClusterConfig
	if kubeconfig == "" {
		kubeconfig = os.Getenv("KUBECONFIG")
	}

	if kubeconfig != "" {
		config, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
	} else {
		config, err = rest.InClusterConfig()
	}
	if err != nil {
		log.Fatalf("error creating client: %v", err)
	}
	client := kubernetes.NewForConfigOrDie(config)
	awsSession := session.Must(session.NewSession())
	ssmSvc := ssm.New(awsSession)

	log.Print("Processing Parameters")
	// aws takes a slice of *string. Need to migrate ssmParams
	paramPtrs := copyParamPtrs(&ssmParams)
	log.Println("fetching from AWS:")
	for i, p := range *paramPtrs {
		log.Printf("  param buffer[%d]:[%s]\n", i, *p)
	}

	for _, v := range getParamsFromAWS(ssmSvc, paramPtrs).Parameters {
		err = crateSecret(client, namespace, v)
		if err != nil {
			// change this line if you want to support ignoring failed secret creation
			log.Fatalf("Error creating secret. Terminating")
		}
	}
}

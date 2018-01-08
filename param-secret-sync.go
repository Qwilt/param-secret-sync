package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ssm"
	"github.com/nirdothan/param-secret-sync/version"
	apicorev1 "k8s.io/api/core/v1"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// left trim the string until the last slash char
func getSecretNameFromParam(name string) string {
	return name[strings.LastIndex(name, "/")+1:]
}

func crateSecret(client *kubernetes.Clientset, namespace string, param *ssm.Parameter) error {

	// were going to use this name as both the secret object name and
	// the .data key name (for lack of a better generic solution)
	name := getSecretNameFromParam(*(param.Name))
	secret := &apicorev1.Secret{
		ObjectMeta: meta_v1.ObjectMeta{
			Name: name,
			Labels: map[string]string{
				"heritage": "param-secret-sync",
			},
		},
		Type: "Opaque",
		StringData: map[string]string{
			name: *param.Value,
		},
	}
	log.Printf("creating secret [%s] in namespace [%s]", name, namespace)
	_, err := client.CoreV1().Secrets(namespace).Create(secret)
	if err != nil {
		log.Printf("Failed to create secret [%s] in kubernetes[%v]",
			getSecretNameFromParam(*(param.Name)), err)
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
		fmt.Fprintf(os.Stderr, "No param names provided!")
		os.Exit(1)
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
		fmt.Fprintf(os.Stderr, "error creating client: %v", err)
		os.Exit(1)
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

	params := &ssm.GetParametersInput{
		Names:          *paramPtrs,
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

	for _, v := range vals.Parameters {
		err = crateSecret(client, namespace, v)
	}
}

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

	paramList, kubeconfig := "", ""
	flag.StringVar(&paramList, "params", paramList, "comma separated list of param names")
	flag.StringVar(&kubeconfig, "kubeconfig", kubeconfig, "kubeconfig file")
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

	//client.secretLister.Secrets("nird").List(labels.Everything())
	//Core().V1().Secrets()

	secret := &apicorev1.Secret{
		ObjectMeta: meta_v1.ObjectMeta{
			Name: "test-secret",
		},
		Type:       "Opaque",
		StringData: map[string]string{"test": "test"},
	}

	client.CoreV1().Secrets("nird").Create(secret)

	//deployments, err := client.AppsV1beta1().Deployments("default").List(meta_v1.ListOptions{})

	// for _, sec := range secrets.Items {
	// 	fmt.Printf("secret %v\n", sec.Name)
	// }

}

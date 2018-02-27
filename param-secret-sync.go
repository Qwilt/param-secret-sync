package main

import (
	"encoding/json"
	"flag"
	"log"
	"os"
	"strings"

	"github.com/Qwilt/param-secret-sync/pkg/version"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ssm"
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

func parseParamVal(jsonString string) *map[string][]byte {
	//var s map[string][]byte
	s := make(map[string][]byte)
	err := json.Unmarshal([]byte(jsonString), &s)
	if err != nil {
		log.Fatalf("Canot unmarshall json param [%s]\n%s", jsonString, err)
	}
	return &s
}

func createSecret(client *kubernetes.Clientset, namespace string, name string, value *map[string][]byte, params secretDescriptors) error {
	var secType apicorev1.SecretType
	for _, p := range params {
		if p.secshortname == name {
			secType = p.sectype
			break
		}
	}
	if secType == "" {
		log.Fatalf("can't find secret type for secret [%v]. This is probably a bug!", name)
	}
	secret := &apicorev1.Secret{
		ObjectMeta: meta_v1.ObjectMeta{
			Name: name,
			Labels: map[string]string{
				"heritage": "param-secret-sync",
			},
		},
		Type: secType,
		Data: *value,
	}

	log.Printf("creating secret [%s] in namespace [%s]", name, namespace)
	_, err := client.CoreV1().Secrets(namespace).Create(secret)
	if err != nil {
		if errors.IsAlreadyExists(err) {
			log.Printf("Secret [%s] already exists. Trying to update, but some fields may be immutable", name)
			_, err = client.CoreV1().Secrets(namespace).Update(secret)
			if err != nil {
				log.Fatal(err.Error())
			}
			log.Printf("Secret [%s] updated sucesffully", name)
			return nil
		}

		log.Printf("Failed to create secret [%s] in kubernetes", name)

		return err
	}
	log.Printf("Secret [%s] created sucesffully", name)
	return nil

}
func copyParamPtrs(s secretDescriptors) *[]*string {
	t := make([]*string, len(s))

	for i := 0; i < len(s); i++ {
		//t[i] = &((*s)[i])
		t[i] = &(s[i].secfullpath)
	}
	return &t
}

type secDesc struct {
	secfullpath  string
	secshortname string
	sectype      apicorev1.SecretType
}
type secretDescriptors []secDesc

func (i *secretDescriptors) Set(value string) error {
	sectuple := strings.Split(value, ":")
	// if user didn't specify secret type apply default type Opaque
	if len(sectuple) < 2 {
		sectuple = append(sectuple, string(apicorev1.SecretTypeOpaque))
	}
	*i = append(*i, secDesc{
		secfullpath:  sectuple[0],
		sectype:      apicorev1.SecretType(sectuple[1]),
		secshortname: getSecretNameFromParam(sectuple[0]),
	})
	return nil
}
func (i *secretDescriptors) String() string {
	str := ""
	for _, v := range *i {
		str += (v.secfullpath + ",")
	}
	return str
}

var params secretDescriptors

func main() {

	log.Printf("param-secret-sync version %s", version.VERSION)
	var (
		config *rest.Config
		err    error
	)

	kubeconfig, namespace := "", "default"
	flag.Var(&params, "param", `param name and optional secret type separated by colon e.g.
						/vault/mydockerlogin:kubernetes.io/dockercfg`)
	flag.StringVar(&kubeconfig, "kubeconfig", kubeconfig, "kubeconfig file")
	flag.StringVar(&namespace, "namespace", namespace, "target secret namespace")
	flag.Parse()

	if len(params) == 0 {
		log.Fatal("No param names provided!")
	}

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
	// aws takes a slice of *string. take their addresses from params
	paramPtrs := copyParamPtrs(params)
	log.Println("fetching from AWS:")
	for i, p := range *paramPtrs {
		log.Printf("  param buffer[%d]:[%s]\n", i, *p)
	}

	paramResponse := getParamsFromAWS(ssmSvc, paramPtrs)

	// the format of paramVals is:
	// {   secret_name1:
	//	         { key_11: val_11, key_12: val_12 },
	//     secret_name2:
	//           { key_21: val_21, key22: val_22 }
	// }
	paramVals := make(map[string]map[string][]byte)
	// iterate over values, parse them and validate
	for _, p := range paramResponse.Parameters {
		//extract the last part of the name path to decode the secret name
		key := getSecretNameFromParam(*p.Name)
		tmp := *parseParamVal(*p.Value)
		paramVals[key] = tmp
	}

	for k, v := range paramVals {

		err = createSecret(client, namespace, k, &v, params)
		if err != nil {
			// change this line if you want to support ignoring failed secret creation
			log.Fatalf("Error creating secret. Terminating")
		}
	}
}

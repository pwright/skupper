package main

import (
	"io/ioutil"
	"log"
	"os"

	"k8s.io/kubernetes/pkg/skupper/cmd                                                                "
	cmdutil "k8s.io/kubernetes/pkg/skupper/cmd/util"

	"github.com/spf13/cobra/doc"
)

func main() {
	skupper := cmd.NewskupperCommand(cmdutil.NewFactory(nil), os.Stdin, ioutil.Discard, ioutil.Discard)
	err := doc.GenMarkdownTree(skupper, "./markdown")
	if err != nil {
		log.Fatal(err)
	}
}
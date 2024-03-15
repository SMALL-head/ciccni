package logUtils

import (
	"github.com/sirupsen/logrus"
	"os"
)

var Log = logrus.New()

const LogFilename = "/root/ciccni/log/cni.log"

func init() {
	file, err := os.OpenFile(LogFilename, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		Log.Out = os.Stdout
	}
	Log.Out = file
}

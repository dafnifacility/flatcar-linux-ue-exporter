package main

import (
	"context"
	"fmt"
	"time"

	"github.com/flatcar-linux/flatcar-linux-update-operator/pkg/updateengine"
	log "github.com/sirupsen/logrus"
)

func main() {
	ctx := context.Background()
	log.Info("creating update engine client")
	cl, err := updateengine.New()
	if err != nil {
		log.WithError(err).Fatal("unable to create engine client")
	}
	uc := make(chan updateengine.Status, 1)
	toc, cf := context.WithTimeout(ctx, 5*time.Second)
	defer cf()
	cl.ReceiveStatuses(uc, toc.Done())
	log.Info("waiting for update on status channel")
	s := <-uc
	fmt.Println(s.String())
}

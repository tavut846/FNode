package node

import (
	"strconv"

	"github.com/tavut846/FNode/api/panel"
	log "github.com/sirupsen/logrus"
)

func (c *Controller) reportUserTrafficTask() (err error) {
	var userTraffic []panel.UserTraffic
	if c.server != nil {
		userTraffic, _ = c.server.GetUserTrafficSlice(c.tag, true)
		if len(userTraffic) > 0 {
			err = c.apiClient.ReportUserTraffic(userTraffic)
			if err != nil {
				log.WithFields(log.Fields{
					"tag": c.tag,
					"err": err,
				}).Info("Report user traffic failed")
			} else {
				log.WithField("tag", c.tag).Infof("Report %d users traffic", len(userTraffic))
				log.WithField("tag", c.tag).Debugf("User traffic: %+v", userTraffic)
			}
		}
	}

	onlineDevice, err := c.limiter.GetOnlineDevice()
	if err != nil {
		log.Print(err)
	}

	currentUIDs := make(map[int]struct{})
	data := make(map[int][]string)

	if onlineDevice != nil && len(*onlineDevice) > 0 {
		var nocountUID = make(map[int]struct{})
		if c.Options.DeviceOnlineMinTraffic > 0 {
			for _, traffic := range userTraffic {
				total := traffic.Upload + traffic.Download
				if total < int64(c.Options.DeviceOnlineMinTraffic*1000) {
					nocountUID[traffic.UID] = struct{}{}
				}
			}
		}
		for _, online := range *onlineDevice {
			if _, ok := nocountUID[online.UID]; !ok {
				data[online.UID] = append(data[online.UID], online.IP)
				currentUIDs[online.UID] = struct{}{}
			}
		}
	}

	// For any UID previously reported online on this node that has no active devices now,
	// send an empty array [] so Xboard immediately clears the old devices from Redis instead of waiting 5 minutes.
	for prevUID := range c.lastReportedUIDs {
		if _, ok := currentUIDs[prevUID]; !ok {
			data[prevUID] = []string{}
		}
	}

	if len(data) > 0 {
		if err = c.apiClient.ReportNodeOnlineUsers(&data); err != nil {
			log.WithFields(log.Fields{
				"tag": c.tag,
				"err": err,
			}).Info("Report online users failed")
		} else {
			activeReportedCount := len(currentUIDs)
			log.WithField("tag", c.tag).Infof("Total %d online users reported", activeReportedCount)
			log.WithField("tag", c.tag).Debugf("Online users: %+v", data)
			c.lastReportedUIDs = currentUIDs
		}
	} else {
		c.lastReportedUIDs = currentUIDs
	}

	userTraffic = nil
	return nil
}

func compareUserList(old, new []panel.UserInfo) (deleted, added []panel.UserInfo) {
	oldMap := make(map[string]int)
	for i, user := range old {
		key := user.Uuid + strconv.Itoa(user.SpeedLimit)
		oldMap[key] = i
	}

	for _, user := range new {
		key := user.Uuid + strconv.Itoa(user.SpeedLimit)
		if _, exists := oldMap[key]; !exists {
			added = append(added, user)
		} else {
			delete(oldMap, key)
		}
	}

	for _, index := range oldMap {
		deleted = append(deleted, old[index])
	}

	return deleted, added
}

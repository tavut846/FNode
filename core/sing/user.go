package sing

import (
	"encoding/base64"
	"errors"

	"github.com/tavut846/FNode/api/panel"
	"github.com/tavut846/FNode/common/counter"
	"github.com/tavut846/FNode/core"
	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/option"
)

func (b *Sing) AddUsers(p *core.AddUsersParams) (added int, err error) {
	b.users.mapLock.Lock()
	defer b.users.mapLock.Unlock()

	for i := range p.Users {
		b.users.uidMap[p.Users[i].Uuid] = p.Users[i].Id
	}

	if _, ok := b.inboundUsers[p.Tag]; !ok {
		b.inboundUsers[p.Tag] = make([]panel.UserInfo, 0)
	}
	b.inboundUsers[p.Tag] = append(b.inboundUsers[p.Tag], p.Users...)

	if p.NodeInfo != nil {
		b.inboundInfo[p.Tag] = p.NodeInfo
	}

	return len(p.Users), b.updateInboundUsers(p.Tag)
}

func (b *Sing) DelUsers(users []panel.UserInfo, tag string, _ *panel.NodeInfo) error {
	b.users.mapLock.Lock()
	defer b.users.mapLock.Unlock()

	removeSet := make(map[string]struct{}, len(users))
	for i := range users {
		removeSet[users[i].Uuid] = struct{}{}

		if v, ok := b.hookServer.counter.Load(tag); ok {
			c := v.(*counter.TrafficCounter)
			c.Delete(users[i].Uuid)
		}
		delete(b.users.uidMap, users[i].Uuid)
	}

	if currentUsers, ok := b.inboundUsers[tag]; ok {
		var kept []panel.UserInfo
		for _, u := range currentUsers {
			if _, rm := removeSet[u.Uuid]; !rm {
				kept = append(kept, u)
			}
		}
		b.inboundUsers[tag] = kept
	}

	return b.updateInboundUsers(tag)
}

func (b *Sing) updateInboundUsers(tag string) error {
	in, found := b.box.Inbound().Get(tag)
	if !found {
		return errors.New("the inbound not found")
	}
	users := b.inboundUsers[tag]
	info := b.inboundInfo[tag]
	if info == nil {
		return errors.New("node info not found for tag: " + tag)
	}

	switch info.Type {
	case "vless":
		us := make([]option.VLESSUser, len(users))
		for i := range users {
			us[i] = option.VLESSUser{
				Name: users[i].Uuid,
				Flow: info.VAllss.Flow,
				UUID: users[i].Uuid,
			}
		}
		if u, ok := in.(adapter.UpdatableInbound[option.VLESSUser]); ok {
			return u.UpdateUsers(us)
		}
	case "vmess":
		us := make([]option.VMessUser, len(users))
		for i := range users {
			us[i] = option.VMessUser{
				Name: users[i].Uuid,
				UUID: users[i].Uuid,
			}
		}
		if u, ok := in.(adapter.UpdatableInbound[option.VMessUser]); ok {
			return u.UpdateUsers(us)
		}
	case "shadowsocks":
		us := make([]option.ShadowsocksUser, len(users))
		for i := range users {
			var password = users[i].Uuid
			switch info.Shadowsocks.Cipher {
			case "2022-blake3-aes-128-gcm":
				password = base64.StdEncoding.EncodeToString([]byte(password[:16]))
			case "2022-blake3-aes-256-gcm":
				password = base64.StdEncoding.EncodeToString([]byte(password[:32]))
			}
			us[i] = option.ShadowsocksUser{
				Name:     users[i].Uuid,
				Password: password,
			}
		}
		if u, ok := in.(adapter.UpdatableShadowsocksInbound); ok {
			return u.UpdateUsersByOptions(us)
		}
	case "trojan":
		us := make([]option.TrojanUser, len(users))
		for i := range users {
			us[i] = option.TrojanUser{
				Name:     users[i].Uuid,
				Password: users[i].Uuid,
			}
		}
		if u, ok := in.(adapter.UpdatableInbound[option.TrojanUser]); ok {
			return u.UpdateUsers(us)
		}
	case "tuic":
		us := make([]option.TUICUser, len(users))
		for i := range users {
			us[i] = option.TUICUser{
				Name:     users[i].Uuid,
				UUID:     users[i].Uuid,
				Password: users[i].Uuid,
			}
		}
		if u, ok := in.(adapter.UpdatableInbound[option.TUICUser]); ok {
			return u.UpdateUsers(us)
		}
	case "hysteria":
		us := make([]option.HysteriaUser, len(users))
		for i := range users {
			us[i] = option.HysteriaUser{
				Name:       users[i].Uuid,
				AuthString: users[i].Uuid,
			}
		}
		if u, ok := in.(adapter.UpdatableInbound[option.HysteriaUser]); ok {
			return u.UpdateUsers(us)
		}
	case "hysteria2":
		us := make([]option.Hysteria2User, len(users))
		for i := range users {
			us[i] = option.Hysteria2User{
				Name:     users[i].Uuid,
				Password: users[i].Uuid,
			}
		}
		if u, ok := in.(adapter.UpdatableInbound[option.Hysteria2User]); ok {
			return u.UpdateUsers(us)
		}
	case "anytls":
		us := make([]option.AnyTLSUser, len(users))
		for i := range users {
			us[i] = option.AnyTLSUser{
				Name:     users[i].Uuid,
				Password: users[i].Uuid,
			}
		}
		if u, ok := in.(adapter.UpdatableInbound[option.AnyTLSUser]); ok {
			return u.UpdateUsers(us)
		}
	}
	return errors.New("unsupported inbound type for dynamic users or inbound does not support UpdatableInbound")
}

func (b *Sing) GetUserTraffic(tag, uuid string, reset bool) (up int64, down int64) {
	if v, ok := b.hookServer.counter.Load(tag); ok {
		c := v.(*counter.TrafficCounter)
		up = c.GetUpCount(uuid)
		down = c.GetDownCount(uuid)
		if reset {
			c.Reset(uuid)
		}
		return
	}
	return 0, 0
}

func (b *Sing) GetUserTrafficSlice(tag string, reset bool) ([]panel.UserTraffic, error) {
	trafficSlice := make([]panel.UserTraffic, 0)
	hook := b.hookServer
	b.users.mapLock.RLock()
	defer b.users.mapLock.RUnlock()
	if v, ok := hook.counter.Load(tag); ok {
		c := v.(*counter.TrafficCounter)
		c.Counters.Range(func(key, value interface{}) bool {
			uuid := key.(string)
			traffic := value.(*counter.TrafficStorage)
			up := traffic.UpCounter.Load()
			down := traffic.DownCounter.Load()
			if up+down > b.nodeReportMinTrafficBytes[tag] {
				if reset {
					traffic.UpCounter.Store(0)
					traffic.DownCounter.Store(0)
				}
				if b.users.uidMap[uuid] == 0 {
					c.Delete(uuid)
					return true
				}
				trafficSlice = append(trafficSlice, panel.UserTraffic{
					UID:      b.users.uidMap[uuid],
					Upload:   up,
					Download: down,
				})
			}
			return true
		})
		if len(trafficSlice) == 0 {
			return nil, nil
		}
		return trafficSlice, nil
	}
	return nil, nil
}

package sing

import (
	"encoding/base64"
	"errors"

	"github.com/tavut846/FNode/api/panel"
	"github.com/tavut846/FNode/common/counter"
	"github.com/tavut846/FNode/common/format"
	"github.com/tavut846/FNode/core"
	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/option"
	F "github.com/sagernet/sing/common/format"
)

func (b *Sing) AddUsers(p *core.AddUsersParams) (added int, err error) {
	b.users.mapLock.Lock()
	defer b.users.mapLock.Unlock()

	for i := range p.Users {
		uuid := p.Users[i].Uuid
		id := p.Users[i].Id
		b.users.uidMap[uuid] = id
		b.users.uidMap[format.UserTag(p.Tag, uuid)] = id
		b.users.uidMap[format.UserEmail(p.Tag, uuid)] = id
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
		uuid := users[i].Uuid
		removeSet[uuid] = struct{}{}

		if v, ok := b.hookServer.counter.Load(tag); ok {
			c := v.(*counter.TrafficCounter)
			c.Delete(uuid)
			c.Delete(format.UserTag(tag, uuid))
			c.Delete(format.UserEmail(tag, uuid))
		}
		delete(b.users.uidMap, uuid)
		delete(b.users.uidMap, format.UserTag(tag, uuid))
		delete(b.users.uidMap, format.UserEmail(tag, uuid))
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
				Name: format.UserEmail(tag, users[i].Uuid),
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
				Name: format.UserEmail(tag, users[i].Uuid),
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
				Name:     format.UserEmail(tag, users[i].Uuid),
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
				Name:     format.UserEmail(tag, users[i].Uuid),
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
				Name:     format.UserEmail(tag, users[i].Uuid),
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
				Name:       format.UserEmail(tag, users[i].Uuid),
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
				Name:     format.UserEmail(tag, users[i].Uuid),
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
				Name:     format.UserEmail(tag, users[i].Uuid),
				Password: users[i].Uuid,
			}
		}
		if u, ok := in.(adapter.UpdatableInbound[option.AnyTLSUser]); ok {
			if err := u.UpdateUsers(us); err == nil {
				return nil
			}
		}
		return b.recreateInbound(tag, info)
	}
	return errors.New("unsupported inbound type for dynamic users or inbound does not support UpdatableInbound")
}

func (b *Sing) recreateInbound(tag string, info *panel.NodeInfo) error {
	in := b.box.Inbound()
	_ = in.Remove(tag)
	cfg := b.inboundConfig[tag]
	c, err := b.getInboundOptions(tag, info, cfg)
	if err != nil {
		return err
	}
	return in.Create(
		b.ctx,
		b.box.Router(),
		b.logFactory.NewLogger(F.ToString("inbound/", c.Type, "[", tag, "]")),
		tag,
		c.Type,
		c.Options,
	)
}

func (b *Sing) GetUserTraffic(tag, uuid string, reset bool) (up int64, down int64) {
	if v, ok := b.hookServer.counter.Load(tag); ok {
		c := v.(*counter.TrafficCounter)
		cleanUUID := format.ExtractUUID(uuid)
		up = c.GetUpCount(cleanUUID)
		down = c.GetDownCount(cleanUUID)
		if up == 0 && down == 0 && cleanUUID != uuid {
			up = c.GetUpCount(uuid)
			down = c.GetDownCount(uuid)
		}
		if reset {
			c.Reset(cleanUUID)
			if cleanUUID != uuid {
				c.Reset(uuid)
			}
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
			rawKey := key.(string)
			cleanUUID := format.ExtractUUID(rawKey)
			traffic := value.(*counter.TrafficStorage)
			up := traffic.UpCounter.Load()
			down := traffic.DownCounter.Load()
			if up+down > b.nodeReportMinTrafficBytes[tag] {
				if reset {
					traffic.UpCounter.Store(0)
					traffic.DownCounter.Store(0)
				}
				uid := b.users.uidMap[cleanUUID]
				if uid == 0 {
					uid = b.users.uidMap[rawKey]
				}
				if uid == 0 {
					c.Delete(rawKey)
					return true
				}
				trafficSlice = append(trafficSlice, panel.UserTraffic{
					UID:      uid,
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

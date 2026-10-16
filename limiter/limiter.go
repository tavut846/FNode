package limiter

import (
	"errors"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/tavut846/FNode/api/panel"
	"github.com/tavut846/FNode/common/format"
	"github.com/tavut846/FNode/conf"
	"github.com/juju/ratelimit"
)

var limitLock sync.RWMutex
var limiter = map[string]*Limiter{}

func Init() {
	limitLock.Lock()
	defer limitLock.Unlock()
	limiter = map[string]*Limiter{}
}

type Limiter struct {
	DomainRules   []*regexp.Regexp
	ProtocolRules []string
	SpeedLimit    int
	UserOnlineIP  *sync.Map      // Key: TagUUID, value: {Key: Ip, value: Uid}
	OldUserOnline *sync.Map      // Key: Ip, value: Uid
	UUIDtoUID     map[string]int // Key: UUID, value: Uid
	UserLimitInfo *sync.Map      // Key: TagUUID value: UserLimitInfo
	SpeedLimiter  *sync.Map      // key: TagUUID, value: *ratelimit.Bucket
	AliveList     map[int]int    // Key: Uid, value: alive_ip
}

type UserLimitInfo struct {
	UID               int
	SpeedLimit        int
	DeviceLimit       int
	DynamicSpeedLimit int
	ExpireTime        int64
	OverLimit         bool
}

func AddLimiter(tag string, l *conf.LimitConfig, users []panel.UserInfo, aliveList map[int]int) *Limiter {
	info := &Limiter{
		SpeedLimit:    l.SpeedLimit,
		UserOnlineIP:  new(sync.Map),
		UserLimitInfo: new(sync.Map),
		SpeedLimiter:  new(sync.Map),
		AliveList:     aliveList,
		OldUserOnline: new(sync.Map),
	}
	uuidmap := make(map[string]int)
	for i := range users {
		uuidmap[users[i].Uuid] = users[i].Id
		userLimit := &UserLimitInfo{}
		userLimit.UID = users[i].Id
		if users[i].SpeedLimit != 0 {
			userLimit.SpeedLimit = users[i].SpeedLimit
		}
		if users[i].DeviceLimit != 0 {
			userLimit.DeviceLimit = users[i].DeviceLimit
		}
		userLimit.OverLimit = false
		info.UserLimitInfo.Store(format.UserTag(tag, users[i].Uuid), userLimit)
	}
	info.UUIDtoUID = uuidmap
	limitLock.Lock()
	limiter[tag] = info
	limitLock.Unlock()
	return info
}

func GetLimiter(tag string) (info *Limiter, err error) {
	limitLock.RLock()
	info, ok := limiter[tag]
	limitLock.RUnlock()
	if !ok {
		return nil, errors.New("not found")
	}
	return info, nil
}

func DeleteLimiter(tag string) {
	limitLock.Lock()
	delete(limiter, tag)
	limitLock.Unlock()
}

func (l *Limiter) UpdateUser(tag string, added []panel.UserInfo, deleted []panel.UserInfo) {
	for i := range deleted {
		l.UserLimitInfo.Delete(format.UserTag(tag, deleted[i].Uuid))
		l.UserOnlineIP.Delete(format.UserTag(tag, deleted[i].Uuid))
		l.SpeedLimiter.Delete(format.UserTag(tag, deleted[i].Uuid))
		delete(l.UUIDtoUID, deleted[i].Uuid)
		delete(l.AliveList, deleted[i].Id)
	}
	for i := range added {
		userLimit := &UserLimitInfo{
			UID: added[i].Id,
		}
		if added[i].SpeedLimit != 0 {
			userLimit.SpeedLimit = added[i].SpeedLimit
			userLimit.ExpireTime = 0
		}
		if added[i].DeviceLimit != 0 {
			userLimit.DeviceLimit = added[i].DeviceLimit
		}
		userLimit.OverLimit = false
		l.UserLimitInfo.Store(format.UserTag(tag, added[i].Uuid), userLimit)
		l.UUIDtoUID[added[i].Uuid] = added[i].Id
	}
}

func (l *Limiter) UpdateDynamicSpeedLimit(tag, uuid string, limit int, expire time.Time) error {
	if v, ok := l.UserLimitInfo.Load(format.UserTag(tag, uuid)); ok {
		info := v.(*UserLimitInfo)
		info.DynamicSpeedLimit = limit
		info.ExpireTime = expire.Unix()
	} else {
		return errors.New("not found")
	}
	return nil
}

type IPRecord struct {
	UID      int
	LastSeen int64
}

func (l *Limiter) CheckLimit(taguuid string, ip string, isTcp bool, noSSUDP bool) (Bucket *ratelimit.Bucket, Reject bool) {
	// check if ipv4 mapped ipv6
	ip = strings.TrimPrefix(ip, "::ffff:")

	// check and gen speed limit Bucket
	nodeLimit := l.SpeedLimit
	userLimit := 0
	deviceLimit := 0
	var uid int
	if v, ok := l.UserLimitInfo.Load(taguuid); ok {
		u := v.(*UserLimitInfo)
		deviceLimit = u.DeviceLimit
		uid = u.UID
		if u.ExpireTime < time.Now().Unix() && u.ExpireTime != 0 {
			u.DynamicSpeedLimit = 0
			u.ExpireTime = 0
			userLimit = u.SpeedLimit
		} else {
			userLimit = determineSpeedLimit(u.SpeedLimit, u.DynamicSpeedLimit)
		}
	} else {
		return nil, true
	}
	if noSSUDP {
		now := time.Now().Unix()
		const ipActiveTimeout = 60

		var ipMap *sync.Map
		if v, loaded := l.UserOnlineIP.Load(taguuid); loaded {
			ipMap = v.(*sync.Map)
		} else {
			ipMap = new(sync.Map)
			actual, _ := l.UserOnlineIP.LoadOrStore(taguuid, ipMap)
			ipMap = actual.(*sync.Map)
		}

		if v, exists := ipMap.Load(ip); exists {
			if rec, ok := v.(*IPRecord); ok {
				rec.LastSeen = now
			} else {
				ipMap.Store(ip, &IPRecord{UID: uid, LastSeen: now})
			}
		} else {
			activeCount := 0
			var oldestIP string
			var oldestSeen int64 = now + 1

			ipMap.Range(func(k, v interface{}) bool {
				var recLastSeen int64
				if rec, ok := v.(*IPRecord); ok {
					recLastSeen = rec.LastSeen
				}
				if recLastSeen > 0 && now-recLastSeen > ipActiveTimeout {
					ipMap.Delete(k)
				} else {
					activeCount++
					if recLastSeen > 0 && recLastSeen < oldestSeen {
						oldestSeen = recLastSeen
						oldestIP = k.(string)
					}
				}
				return true
			})

			aliveIp := l.AliveList[uid]
			if deviceLimit > 0 {
				otherNodes := aliveIp - activeCount
				if otherNodes < 0 {
					otherNodes = 0
				}
				if otherNodes >= deviceLimit {
					return nil, true
				}
				if activeCount >= deviceLimit {
					if oldestIP != "" {
						ipMap.Delete(oldestIP)
					}
				}
			}

			ipMap.Store(ip, &IPRecord{
				UID:      uid,
				LastSeen: now,
			})
		}
	}

	limit := int64(determineSpeedLimit(nodeLimit, userLimit)) * 1000000 / 8 // If you need the Speed limit
	if limit > 0 {
		Bucket = ratelimit.NewBucketWithQuantum(time.Second, limit, limit) // Byte/s
		if v, ok := l.SpeedLimiter.LoadOrStore(taguuid, Bucket); ok {
			return v.(*ratelimit.Bucket), false
		} else {
			l.SpeedLimiter.Store(taguuid, Bucket)
			return Bucket, false
		}
	} else {
		return nil, false
	}
}

func (l *Limiter) GetOnlineDevice() (*[]panel.OnlineUser, error) {
	var onlineUser []panel.OnlineUser
	now := time.Now().Unix()
	const ipActiveTimeout = 60

	l.UserOnlineIP.Range(func(key, value interface{}) bool {
		taguuid := key.(string)
		ipMap := value.(*sync.Map)
		hasActive := false

		ipMap.Range(func(k, v interface{}) bool {
			ip := k.(string)
			if rec, ok := v.(*IPRecord); ok {
				if now-rec.LastSeen <= ipActiveTimeout {
					onlineUser = append(onlineUser, panel.OnlineUser{UID: rec.UID, IP: ip})
					hasActive = true
				} else {
					ipMap.Delete(k)
				}
			} else if uid, ok := v.(int); ok {
				onlineUser = append(onlineUser, panel.OnlineUser{UID: uid, IP: ip})
				hasActive = true
			}
			return true
		})

		if !hasActive {
			l.UserOnlineIP.Delete(taguuid)
		}
		return true
	})

	return &onlineUser, nil
}

type UserIpList struct {
	Uid    int      `json:"Uid"`
	IpList []string `json:"Ips"`
}

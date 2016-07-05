package status

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/mongodb/mongo-tools/common/text"
	"github.com/mongodb/mongo-tools/common/util"
)

type LockUsage struct {
	Namespace string
	Reads     int64
	Writes    int64
}

type lockUsages []LockUsage

func (slice lockUsages) Len() int {
	return len(slice)
}

func (slice lockUsages) Less(i, j int) bool {
	return slice[i].Reads+slice[i].Writes < slice[j].Reads+slice[j].Writes
}

func (slice lockUsages) Swap(i, j int) {
	slice[i], slice[j] = slice[j], slice[i]
}

func percentageInt64(value, outOf int64) float64 {
	if value == 0 || outOf == 0 {
		return 0
	}
	return 100 * (float64(value) / float64(outOf))
}

func averageInt64(value, outOf int64) int64 {
	if value == 0 || outOf == 0 {
		return 0
	}
	return value / outOf
}

func parseLocks(stat *ServerStatus) map[string]LockUsage {
	returnVal := map[string]LockUsage{}
	for namespace, lockInfo := range stat.Locks {
		returnVal[namespace] = LockUsage{
			namespace,
			lockInfo.TimeLockedMicros.Read + lockInfo.TimeLockedMicros.ReadLower,
			lockInfo.TimeLockedMicros.Write + lockInfo.TimeLockedMicros.WriteLower,
		}
	}
	return returnVal
}

func computeLockDiffs(prevLocks, curLocks map[string]LockUsage) []LockUsage {
	lockUsages := lockUsages(make([]LockUsage, 0, len(curLocks)))
	for namespace, curUsage := range curLocks {
		prevUsage, hasKey := prevLocks[namespace]
		if !hasKey {
			// This namespace didn't appear in the previous batch of lock info,
			// so we can't compute a diff for it - skip it.
			continue
		}
		// Calculate diff of lock usage for this namespace and add to the list
		lockUsages = append(lockUsages,
			LockUsage{
				namespace,
				curUsage.Reads - prevUsage.Reads,
				curUsage.Writes - prevUsage.Writes,
			})
	}
	// Sort the array in order of least to most locked
	sort.Sort(lockUsages)
	return lockUsages
}

func diff(newVal, oldVal int64, sampleSecs float64) int64 {
	return int64(float64(newVal-oldVal) / sampleSecs)
}

func diffOp(newStat, oldStat *ServerStatus, f func(*OpcountStats) int64, both bool) string {
	sampleSecs := float64(newStat.SampleTime.Sub(oldStat.SampleTime).Seconds())
	var opcount int64
	var opcountRepl int64
	if newStat.Opcounters != nil && oldStat.Opcounters != nil {
		opcount = diff(f(newStat.Opcounters), f(oldStat.Opcounters), sampleSecs)
	}
	if newStat.OpcountersRepl != nil && oldStat.OpcountersRepl != nil {
		opcountRepl = diff(f(newStat.OpcountersRepl), f(oldStat.OpcountersRepl), sampleSecs)
	}
	switch {
	case both || opcount > 0 && opcountRepl > 0:
		return fmt.Sprintf("%v|%v", opcount, opcountRepl)
	case opcount > 0:
		return fmt.Sprintf("%v", opcount)
	case opcountRepl > 0:
		return fmt.Sprintf("*%v", opcountRepl)
	default:
		return "*0"
	}
}

func getStorageEngine(stat *ServerStatus) string {
	val := "mmapv1"
	if stat.StorageEngine != nil && stat.StorageEngine["name"] != "" {
		val = stat.StorageEngine["name"]
	}
	return val
}

func IsMongos(stat *ServerStatus) bool {
	return stat.ShardCursorType != nil || strings.HasPrefix(stat.Process, "mongos")
}

func HasLocks(stat *ServerStatus) bool {
	return ReadLockedDB(stat, stat) != ""
}

func IsReplSet(stat *ServerStatus) (res bool) {
	if stat.Repl != nil {
		isReplSet, ok := stat.Repl.IsReplicaSet.(bool)
		res = (ok && isReplSet) || len(stat.Repl.SetName) > 0
	}
	return
}

func IsMMAP(stat *ServerStatus) bool {
	return getStorageEngine(stat) == "mmapv1"
}

func IsWT(stat *ServerStatus) bool {
	return getStorageEngine(stat) == "wiredTiger"
}

func ReadHost(newStat, _ *ServerStatus) string {
	return newStat.Host
}

func ReadStorageEngine(newStat, _ *ServerStatus) string {
	return getStorageEngine(newStat)
}

func ReadInsert(newStat, oldStat *ServerStatus) string {
	return diffOp(newStat, oldStat, func(o *OpcountStats) int64 {
		return o.Insert
	}, false)
}

func ReadQuery(newStat, oldStat *ServerStatus) string {
	return diffOp(newStat, oldStat, func(s *OpcountStats) int64 {
		return s.Query
	}, false)
}

func ReadUpdate(newStat, oldStat *ServerStatus) string {
	return diffOp(newStat, oldStat, func(s *OpcountStats) int64 {
		return s.Update
	}, false)
}

func ReadDelete(newStat, oldStat *ServerStatus) string {
	return diffOp(newStat, oldStat, func(s *OpcountStats) int64 {
		return s.Delete
	}, false)
}

func ReadGetMore(newStat, oldStat *ServerStatus) string {
	sampleSecs := float64(newStat.SampleTime.Sub(oldStat.SampleTime).Seconds())
	return fmt.Sprintf("%d", diff(newStat.Opcounters.GetMore, oldStat.Opcounters.GetMore, sampleSecs))
}

func ReadCommand(newStat, oldStat *ServerStatus) string {
	return diffOp(newStat, oldStat, func(s *OpcountStats) int64 {
		return s.Command
	}, true)
}

func ReadDirty(newStat, _ *ServerStatus) (val string) {
	if newStat.WiredTiger != nil {
		bytes := float64(newStat.WiredTiger.Cache.TrackedDirtyBytes)
		max := float64(newStat.WiredTiger.Cache.MaxBytesConfigured)
		if max != 0 {
			val = fmt.Sprintf("%.3f", bytes/max)
		}
	}
	return
}

func ReadUsed(newStat, _ *ServerStatus) (val string) {
	if newStat.WiredTiger != nil {
		bytes := float64(newStat.WiredTiger.Cache.CurrentCachedBytes)
		max := float64(newStat.WiredTiger.Cache.MaxBytesConfigured)
		if max != 0 {
			val = fmt.Sprintf("%.3f", bytes/max)
		}
	}
	return
}

func ReadFlushes(newStat, oldStat *ServerStatus) string {
	var val int64
	if newStat.WiredTiger != nil && oldStat.WiredTiger != nil {
		val = newStat.WiredTiger.Transaction.TransCheckpoints - oldStat.WiredTiger.Transaction.TransCheckpoints
	} else if newStat.BackgroundFlushing != nil && oldStat.BackgroundFlushing != nil {
		val = newStat.BackgroundFlushing.Flushes - oldStat.BackgroundFlushing.Flushes
	}
	return fmt.Sprintf("%d", val)
}

func ReadMapped(newStat, _ *ServerStatus) (val string) {
	if util.IsTruthy(newStat.Mem.Supported) && IsMongos(newStat) {
		val = text.FormatMegabyteAmount(int64(newStat.Mem.Mapped))
	}
	return
}

func ReadVSize(newStat, _ *ServerStatus) (val string) {
	if util.IsTruthy(newStat.Mem.Supported) {
		val = text.FormatMegabyteAmount(int64(newStat.Mem.Virtual))
	}
	return
}

func ReadRes(newStat, _ *ServerStatus) (val string) {
	if util.IsTruthy(newStat.Mem.Supported) {
		val = text.FormatMegabyteAmount(int64(newStat.Mem.Resident))
	}
	return
}

func ReadNonMapped(newStat, _ *ServerStatus) (val string) {
	if util.IsTruthy(newStat.Mem.Supported) && !IsMongos(newStat) {
		val = text.FormatMegabyteAmount(int64(newStat.Mem.Virtual - newStat.Mem.Mapped))
	}
	return
}

func ReadFaults(newStat, oldStat *ServerStatus) string {
	var val int64 = -1
	if oldStat.ExtraInfo != nil && newStat.ExtraInfo != nil &&
		oldStat.ExtraInfo.PageFaults != nil && newStat.ExtraInfo.PageFaults != nil {
		sampleSecs := float64(newStat.SampleTime.Sub(oldStat.SampleTime).Seconds())
		val = diff(*(newStat.ExtraInfo.PageFaults), *(oldStat.ExtraInfo.PageFaults), sampleSecs)
	}
	return fmt.Sprintf("%d", val)
}

func ReadLRW(newStat, oldStat *ServerStatus) (val string) {
	if !IsMongos(newStat) && newStat.Locks != nil && oldStat.Locks != nil {
		global, ok := oldStat.Locks["Global"]
		if ok && global.AcquireCount != nil {
			newColl, inNew := newStat.Locks["Collection"]
			oldColl, inOld := oldStat.Locks["Collection"]
			if inNew && inOld && newColl.AcquireWaitCount != nil && oldColl.AcquireWaitCount != nil {
				rWait := newColl.AcquireWaitCount.Read - oldColl.AcquireWaitCount.Read
				wWait := newColl.AcquireWaitCount.Write - oldColl.AcquireWaitCount.Write
				rTotal := newColl.AcquireCount.Read - oldColl.AcquireCount.Read
				wTotal := newColl.AcquireCount.Write - oldColl.AcquireCount.Write
				r := percentageInt64(rWait, rTotal)
				w := percentageInt64(wWait, wTotal)
				val = fmt.Sprintf("%.1f%%|%.1f%%", r, w)
			}
		}
	}
	return
}

func ReadLRWT(newStat, oldStat *ServerStatus) (val string) {
	if !IsMongos(newStat) && newStat.Locks != nil && oldStat.Locks != nil {
		global, ok := oldStat.Locks["Global"]
		if ok && global.AcquireCount != nil {
			newColl, inNew := newStat.Locks["Collection"]
			oldColl, inOld := oldStat.Locks["Collection"]
			if inNew && inOld && newColl.AcquireWaitCount != nil && oldColl.AcquireWaitCount != nil {
				rWait := newColl.AcquireWaitCount.Read - oldColl.AcquireWaitCount.Read
				wWait := newColl.AcquireWaitCount.Write - oldColl.AcquireWaitCount.Write
				rAcquire := newColl.TimeAcquiringMicros.Read - oldColl.TimeAcquiringMicros.Read
				wAcquire := newColl.TimeAcquiringMicros.Write - oldColl.TimeAcquiringMicros.Write
				r := averageInt64(rAcquire, rWait)
				w := averageInt64(wAcquire, wWait)
				val = fmt.Sprintf("%v|%v", r, w)
			}
		}
	}
	return
}

func ReadLockedDB(newStat, oldStat *ServerStatus) (val string) {
	if !IsMongos(newStat) && newStat.Locks != nil && oldStat.Locks != nil {
		global, ok := oldStat.Locks["Global"]
		if !ok || global.AcquireCount == nil {
			prevLocks := parseLocks(oldStat)
			curLocks := parseLocks(newStat)
			lockdiffs := computeLockDiffs(prevLocks, curLocks)
			db := ""
			var percentage string
			if len(lockdiffs) == 0 {
				if newStat.GlobalLock != nil {
					percentage = fmt.Sprintf("%.1f", percentageInt64(newStat.GlobalLock.LockTime, newStat.GlobalLock.TotalTime))
				}
			} else {
				// Get the entry with the highest lock
				highestLocked := lockdiffs[len(lockdiffs)-1]
				timeDiffMillis := newStat.UptimeMillis - oldStat.UptimeMillis
				lockToReport := highestLocked.Writes

				// if the highest locked namespace is not '.'
				if highestLocked.Namespace != "." {
					for _, namespaceLockInfo := range lockdiffs {
						if namespaceLockInfo.Namespace == "." {
							lockToReport += namespaceLockInfo.Writes
						}
					}
				}

				// lock data is in microseconds and uptime is in milliseconds - so
				// divide by 1000 so that the units match
				lockToReport /= 1000

				db = highestLocked.Namespace
				percentage = fmt.Sprintf("%.1f", percentageInt64(lockToReport, timeDiffMillis))
			}
			if percentage != "" {
				val = fmt.Sprintf("%s:%s%%", db, percentage)
			}
		}
	}
	return
}

func ReadQRW(newStat, _ *ServerStatus) string {
	var qr int64
	var qw int64
	gl := newStat.GlobalLock
	if gl != nil && gl.CurrentQueue != nil {
		// If we have wiredtiger stats, use those instead
		if newStat.WiredTiger != nil {
			qr = gl.CurrentQueue.Readers + gl.ActiveClients.Readers - newStat.WiredTiger.Concurrent.Read.Out
			qw = gl.CurrentQueue.Writers + gl.ActiveClients.Writers - newStat.WiredTiger.Concurrent.Write.Out
			if qr < 0 {
				qr = 0
			}
			if qw < 0 {
				qw = 0
			}
		} else {
			qr = gl.CurrentQueue.Readers
			qw = gl.CurrentQueue.Writers
		}
	}
	return fmt.Sprintf("%v|%v", qr, qw)
}

func ReadARW(newStat, _ *ServerStatus) string {
	var ar int64
	var aw int64
	if gl := newStat.GlobalLock; gl != nil {
		if newStat.WiredTiger != nil {
			ar = newStat.WiredTiger.Concurrent.Read.Out
			aw = newStat.WiredTiger.Concurrent.Write.Out
		} else if newStat.GlobalLock.ActiveClients != nil {
			ar = gl.ActiveClients.Readers
			aw = gl.ActiveClients.Writers
		}
	}
	return fmt.Sprintf("%v|%v", ar, aw)
}

func ReadNetIn(newStat, oldStat *ServerStatus) string {
	sampleSecs := float64(newStat.SampleTime.Sub(oldStat.SampleTime).Seconds())
	val := diff(newStat.Network.BytesIn, oldStat.Network.BytesIn, sampleSecs)
	return text.FormatBits(val)
}

func ReadNetOut(newStat, oldStat *ServerStatus) string {
	sampleSecs := float64(newStat.SampleTime.Sub(oldStat.SampleTime).Seconds())
	val := diff(newStat.Network.BytesOut, oldStat.Network.BytesOut, sampleSecs)
	return text.FormatBits(val)
}

func ReadConn(newStat, _ *ServerStatus) string {
	return fmt.Sprintf("%d", newStat.Connections.Current)
}

func ReadSet(newStat, _ *ServerStatus) (name string) {
	if newStat.Repl != nil {
		name = newStat.Repl.SetName
	}
	return
}

func ReadRepl(newStat, _ *ServerStatus) string {
	switch {
	case newStat.Repl == nil && IsMongos(newStat):
		return "RTR"
	case newStat.Repl == nil:
		return ""
	case util.IsTruthy(newStat.Repl.IsMaster):
		return "PRI"
	case util.IsTruthy(newStat.Repl.Secondary):
		return "SEC"
	case util.IsTruthy(newStat.Repl.IsReplicaSet):
		return "REC"
	case util.IsTruthy(newStat.Repl.ArbiterOnly):
		return "ARB"
	case util.SliceContains(newStat.Repl.Passives, newStat.Repl.Me):
		return "PSV"
	default:
		if !IsReplSet(newStat) {
			return "UNK"
		}
		return "SLV"
	}
}

func ReadTime(newStat, _ *ServerStatus) string {
	return newStat.SampleTime.Format(time.RFC3339)
}
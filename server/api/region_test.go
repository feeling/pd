// Copyright 2017 PingCAP, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// See the License for the specific language governing permissions and
// limitations under the License.

package api

import (
	"fmt"
	"math/rand"
	"net/url"
	"sort"

	. "github.com/pingcap/check"
	"github.com/pingcap/kvproto/pkg/metapb"
	"github.com/pingcap/kvproto/pkg/pdpb"
	"github.com/pingcap/pd/server"
	"github.com/pingcap/pd/server/core"
)

var _ = Suite(&testRegionSuite{})

type testRegionSuite struct {
	svr       *server.Server
	cleanup   cleanUpFunc
	urlPrefix string
}

func (s *testRegionSuite) SetUpSuite(c *C) {
	s.svr, s.cleanup = mustNewServer(c)
	mustWaitLeader(c, []*server.Server{s.svr})

	addr := s.svr.GetAddr()
	s.urlPrefix = fmt.Sprintf("%s%s/api/v1", addr, apiPrefix)

	mustBootstrapCluster(c, s.svr)
}

func (s *testRegionSuite) TearDownSuite(c *C) {
	s.cleanup()
}

func newTestRegionInfo(regionID, storeID uint64, start, end []byte, opts ...core.RegionCreateOption) *core.RegionInfo {
	leader := &metapb.Peer{
		Id:      regionID,
		StoreId: storeID,
	}
	metaRegion := &metapb.Region{
		Id:          regionID,
		StartKey:    start,
		EndKey:      end,
		Peers:       []*metapb.Peer{leader},
		RegionEpoch: &metapb.RegionEpoch{ConfVer: 1, Version: 1},
	}
	newOpts := []core.RegionCreateOption{
		core.SetApproximateKeys(10),
		core.SetApproximateSize(10),
	}
	newOpts = append(newOpts, opts...)
	region := core.NewRegionInfo(metaRegion, leader, newOpts...)
	return region
}

func (s *testRegionSuite) TestRegion(c *C) {
	r := newTestRegionInfo(2, 1, []byte("a"), []byte("b"))
	mustRegionHeartbeat(c, s.svr, r)
	url := fmt.Sprintf("%s/region/id/%d", s.urlPrefix, r.GetID())
	r1 := &regionInfo{}
	err := readJSONWithURL(url, r1)
	c.Assert(err, IsNil)
	c.Assert(r1, DeepEquals, newRegionInfo(r))

	url = fmt.Sprintf("%s/region/key/%s", s.urlPrefix, "a")
	r2 := &regionInfo{}
	err = readJSONWithURL(url, r2)
	c.Assert(err, IsNil)
	c.Assert(r2, DeepEquals, newRegionInfo(r))
}

func (s *testRegionSuite) TestRegionCheck(c *C) {
	r := newTestRegionInfo(2, 1, []byte("a"), []byte("b"))
	downPeer := &metapb.Peer{Id: 13, StoreId: 2}
	r = r.Clone(core.WithAddPeer(downPeer), core.WithDownPeers([]*pdpb.PeerStats{{Peer: downPeer, DownSeconds: 3600}}), core.WithPendingPeers([]*metapb.Peer{downPeer}))
	mustRegionHeartbeat(c, s.svr, r)
	url := fmt.Sprintf("%s/region/id/%d", s.urlPrefix, r.GetID())
	r1 := &regionInfo{}
	err := readJSONWithURL(url, r1)
	c.Assert(err, IsNil)
	c.Assert(r1, DeepEquals, newRegionInfo(r))

	url = fmt.Sprintf("%s/regions/check/%s", s.urlPrefix, "down-peer")
	r2 := &regionsInfo{}
	err = readJSONWithURL(url, r2)
	c.Assert(err, IsNil)
	c.Assert(r2, DeepEquals, &regionsInfo{Count: 1, Regions: []*regionInfo{newRegionInfo(r)}})

	url = fmt.Sprintf("%s/regions/check/%s", s.urlPrefix, "pending-peer")
	r3 := &regionsInfo{}
	err = readJSONWithURL(url, r3)
	c.Assert(err, IsNil)
	c.Assert(r3, DeepEquals, &regionsInfo{Count: 1, Regions: []*regionInfo{newRegionInfo(r)}})
}

func (s *testRegionSuite) TestRegions(c *C) {
	rs := []*core.RegionInfo{
		newTestRegionInfo(2, 1, []byte("a"), []byte("b")),
		newTestRegionInfo(3, 1, []byte("b"), []byte("c")),
		newTestRegionInfo(4, 2, []byte("c"), []byte("d")),
	}
	regions := make([]*regionInfo, 0, len(rs))
	for _, r := range rs {
		regions = append(regions, newRegionInfo(r))
		mustRegionHeartbeat(c, s.svr, r)
	}
	url := fmt.Sprintf("%s/regions", s.urlPrefix)
	regionsInfo := &regionsInfo{}
	err := readJSONWithURL(url, regionsInfo)
	c.Assert(err, IsNil)
	c.Assert(regionsInfo.Count, Equals, len(regions))
	sort.Slice(regionsInfo.Regions, func(i, j int) bool {
		return regionsInfo.Regions[i].ID < regionsInfo.Regions[j].ID
	})
	for i, r := range regionsInfo.Regions {
		c.Assert(r.ID, Equals, regions[i].ID)
		c.Assert(r.ApproximateSize, Equals, regions[i].ApproximateSize)
		c.Assert(r.ApproximateKeys, Equals, regions[i].ApproximateKeys)
	}
}

func (s *testRegionSuite) TestStoreRegions(c *C) {
	r1 := newTestRegionInfo(2, 1, []byte("a"), []byte("b"))
	r2 := newTestRegionInfo(3, 1, []byte("b"), []byte("c"))
	r3 := newTestRegionInfo(4, 2, []byte("c"), []byte("d"))
	mustRegionHeartbeat(c, s.svr, r1)
	mustRegionHeartbeat(c, s.svr, r2)
	mustRegionHeartbeat(c, s.svr, r3)

	regionIDs := []uint64{2, 3}
	url := fmt.Sprintf("%s/regions/store/%d", s.urlPrefix, 1)
	r4 := &regionsInfo{}
	err := readJSONWithURL(url, r4)
	c.Assert(err, IsNil)
	c.Assert(r4.Count, Equals, len(regionIDs))
	sort.Slice(r4.Regions, func(i, j int) bool { return r4.Regions[i].ID < r4.Regions[j].ID })
	for i, r := range r4.Regions {
		c.Assert(r.ID, Equals, regionIDs[i])
	}

	regionIDs = []uint64{4}
	url = fmt.Sprintf("%s/regions/store/%d", s.urlPrefix, 2)
	r5 := &regionsInfo{}
	err = readJSONWithURL(url, r5)
	c.Assert(err, IsNil)
	c.Assert(r5.Count, Equals, len(regionIDs))
	for i, r := range r5.Regions {
		c.Assert(r.ID, Equals, regionIDs[i])
	}

	regionIDs = []uint64{}
	url = fmt.Sprintf("%s/regions/store/%d", s.urlPrefix, 3)
	r6 := &regionsInfo{}
	err = readJSONWithURL(url, r6)
	c.Assert(err, IsNil)
	c.Assert(r6.Count, Equals, len(regionIDs))
}

func (s *testRegionSuite) TestTopFlow(c *C) {
	r1 := newTestRegionInfo(1, 1, []byte("a"), []byte("b"), core.SetWrittenBytes(1000), core.SetReadBytes(1000), core.SetRegionConfVer(1), core.SetRegionVersion(1))
	mustRegionHeartbeat(c, s.svr, r1)
	r2 := newTestRegionInfo(2, 1, []byte("b"), []byte("c"), core.SetWrittenBytes(2000), core.SetReadBytes(0), core.SetRegionConfVer(2), core.SetRegionVersion(3))
	mustRegionHeartbeat(c, s.svr, r2)
	r3 := newTestRegionInfo(3, 1, []byte("c"), []byte("d"), core.SetWrittenBytes(500), core.SetReadBytes(800), core.SetRegionConfVer(3), core.SetRegionVersion(2))
	mustRegionHeartbeat(c, s.svr, r3)
	s.checkTopRegions(c, fmt.Sprintf("%s/regions/writeflow", s.urlPrefix), []uint64{2, 1, 3})
	s.checkTopRegions(c, fmt.Sprintf("%s/regions/readflow", s.urlPrefix), []uint64{1, 3, 2})
	s.checkTopRegions(c, fmt.Sprintf("%s/regions/writeflow?limit=2", s.urlPrefix), []uint64{2, 1})
	s.checkTopRegions(c, fmt.Sprintf("%s/regions/confver", s.urlPrefix), []uint64{3, 2, 1})
	s.checkTopRegions(c, fmt.Sprintf("%s/regions/confver?limit=2", s.urlPrefix), []uint64{3, 2})
	s.checkTopRegions(c, fmt.Sprintf("%s/regions/version", s.urlPrefix), []uint64{2, 3, 1})
	s.checkTopRegions(c, fmt.Sprintf("%s/regions/version?limit=2", s.urlPrefix), []uint64{2, 3})
}

func (s *testRegionSuite) TestTopSize(c *C) {
	opt := core.SetApproximateSize(1000)
	r1 := newTestRegionInfo(7, 1, []byte("a"), []byte("b"), opt)
	mustRegionHeartbeat(c, s.svr, r1)
	opt = core.SetApproximateSize(900)
	r2 := newTestRegionInfo(8, 1, []byte("b"), []byte("c"), opt)
	mustRegionHeartbeat(c, s.svr, r2)
	opt = core.SetApproximateSize(800)
	r3 := newTestRegionInfo(9, 1, []byte("c"), []byte("d"), opt)
	mustRegionHeartbeat(c, s.svr, r3)
	// query with limit
	s.checkTopRegions(c, fmt.Sprintf("%s/regions/size?limit=%d", s.urlPrefix, 2), []uint64{7, 8})
}

func (s *testRegionSuite) checkTopRegions(c *C, url string, regionIDs []uint64) {
	regions := &regionsInfo{}
	err := readJSONWithURL(url, regions)
	c.Assert(err, IsNil)
	c.Assert(regions.Count, Equals, len(regionIDs))
	for i, r := range regions.Regions {
		c.Assert(r.ID, Equals, regionIDs[i])
	}
}

func (s *testRegionSuite) TestTopN(c *C) {
	writtenBytes := []uint64{10, 10, 9, 5, 3, 2, 2, 1, 0, 0}
	for n := 0; n <= len(writtenBytes)+1; n++ {
		regions := make([]*core.RegionInfo, 0, len(writtenBytes))
		for _, i := range rand.Perm(len(writtenBytes)) {
			id := uint64(i + 1)
			region := newTestRegionInfo(id, id, nil, nil, core.SetWrittenBytes(uint64(writtenBytes[i])))
			regions = append(regions, region)
		}
		topN := topNRegions(regions, func(a, b *core.RegionInfo) bool { return a.GetBytesWritten() < b.GetBytesWritten() }, n)
		if n > len(writtenBytes) {
			c.Assert(len(topN), Equals, len(writtenBytes))
		} else {
			c.Assert(len(topN), Equals, n)
		}
		for i := range topN {
			c.Assert(topN[i].GetBytesWritten(), Equals, writtenBytes[i])
		}
	}
}

var _ = Suite(&testGetRegionSuite{})

type testGetRegionSuite struct {
	svr       *server.Server
	cleanup   cleanUpFunc
	urlPrefix string
}

func (s *testGetRegionSuite) SetUpSuite(c *C) {
	s.svr, s.cleanup = mustNewServer(c)
	mustWaitLeader(c, []*server.Server{s.svr})

	addr := s.svr.GetAddr()
	s.urlPrefix = fmt.Sprintf("%s%s/api/v1", addr, apiPrefix)

	mustBootstrapCluster(c, s.svr)
}

func (s *testGetRegionSuite) TearDownSuite(c *C) {
	s.cleanup()
}

func (s *testGetRegionSuite) TestRegionKey(c *C) {
	r := newTestRegionInfo(99, 1, []byte{0xFF, 0xFF, 0xAA}, []byte{0xFF, 0xFF, 0xCC}, core.SetWrittenBytes(500), core.SetReadBytes(800), core.SetRegionConfVer(3), core.SetRegionVersion(2))
	mustRegionHeartbeat(c, s.svr, r)
	url := fmt.Sprintf("%s/region/key/%s", s.urlPrefix, url.QueryEscape(string([]byte{0xFF, 0xFF, 0xBB})))
	regionInfo := &regionInfo{}
	err := readJSONWithURL(url, regionInfo)
	c.Assert(err, IsNil)
	c.Assert(r.GetID(), Equals, regionInfo.ID)
}

func (s *testGetRegionSuite) TestScanRegionByKey(c *C) {
	r1 := newTestRegionInfo(2, 1, []byte("a"), []byte("b"))
	r2 := newTestRegionInfo(3, 1, []byte("b"), []byte("c"))
	r3 := newTestRegionInfo(4, 2, []byte("c"), []byte("d"))
	r := newTestRegionInfo(99, 1, []byte{0xFF, 0xFF, 0xAA}, []byte{0xFF, 0xFF, 0xCC}, core.SetWrittenBytes(500), core.SetReadBytes(800), core.SetRegionConfVer(3), core.SetRegionVersion(2))
	mustRegionHeartbeat(c, s.svr, r1)
	mustRegionHeartbeat(c, s.svr, r2)
	mustRegionHeartbeat(c, s.svr, r3)
	mustRegionHeartbeat(c, s.svr, r)

	url := fmt.Sprintf("%s/regions/key?key=%s", s.urlPrefix, "b")
	regionIds := []uint64{3, 4, 99}
	regions := &regionsInfo{}
	err := readJSONWithURL(url, regions)
	c.Assert(err, IsNil)
	c.Assert(len(regionIds), Equals, regions.Count)
	for i, v := range regionIds {
		c.Assert(v, Equals, regions.Regions[i].ID)
	}
}

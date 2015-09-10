package writer

import (
	"sync"
	"time"
	"strconv"
	"strings"

	"github.com/olehz/imposm3/cache"
	"github.com/olehz/imposm3/database"
	"github.com/olehz/imposm3/element"
	"github.com/olehz/imposm3/expire"
	"github.com/olehz/imposm3/geom"
	"github.com/olehz/imposm3/geom/geos"
	"github.com/olehz/imposm3/mapping"
	"github.com/olehz/imposm3/stats"
)

type RelationWriter struct {
	OsmElemWriter
	singleIdSpace  bool
	rel            chan *element.Relation
	polygonMatcher mapping.RelWayMatcher
	streetMatcher mapping.RelWayMatcher
	maxGap         float64
}

func NewRelationWriter(
	osmCache *cache.OSMCache,
	diffCache *cache.DiffCache,
	singleIdSpace bool,
	rel chan *element.Relation,
	inserter database.Inserter,
	progress *stats.Statistics,
	matcher mapping.RelWayMatcher,
	streetMatcher mapping.RelWayMatcher,
	srid int,
) *OsmElemWriter {
	maxGap := 1e-1 // 0.1m
	if srid == 4326 {
		maxGap = 1e-6 // ~0.1m
	}
	rw := RelationWriter{
		OsmElemWriter: OsmElemWriter{
			osmCache:  osmCache,
			diffCache: diffCache,
			progress:  progress,
			wg:        &sync.WaitGroup{},
			inserter:  inserter,
			srid:      srid,
		},
		singleIdSpace:  singleIdSpace,
		polygonMatcher: matcher,
		streetMatcher:  streetMatcher,
		rel:            rel,
		maxGap:         maxGap,
	}
	rw.OsmElemWriter.writer = &rw
	return &rw.OsmElemWriter
}

func (rw *RelationWriter) relId(id int64) int64 {
	if !rw.singleIdSpace {
		return -id
	}
	return element.RelIdOffset - id
}

func (rw *RelationWriter) loop() {
	geos := geos.NewGeos()
	geos.SetHandleSrid(rw.srid)
	defer geos.Finish()

NextRel:
	for r := range rw.rel {
		rw.progress.AddRelations(1)

		if r.Tags["type"] == "street" || r.Tags["type"] == "associatedStreet" {
			var streets[]string
			var houses[]string
			for _, m := range r.Members {
				if m.Role == "street" && m.Type == 1 {
					streets = append(streets, strconv.FormatInt(m.Id, 10))
				}
				if m.Role == "house" && (m.Type == 1 || m.Type == 2) {
					var id int64
					if m.Type == 2 {
						id = m.Id * -1
					} else {
						id = m.Id
					}
					houses = append(houses, strconv.FormatInt(id, 10))
				}
			}
			if len(streets) > 0 && len(houses) > 0 {
				if len(streets) > 0 {
					r.Tags["streets"] = strings.Join(streets, ", ")
				}
				if len(houses) > 0 {
					r.Tags["houses"] = strings.Join(houses, ", ")
				}

				matches := rw.streetMatcher.MatchRelation(r)
				rel := element.Relation(*r)
				rel.Id = rw.relId(r.Id)
				err := rw.inserter.InsertPoint(rel.OSMElem, matches)
				if err != nil {
					if errl, ok := err.(ErrorLevel); !ok || errl.Level() > 0 {
						log.Warn(err)
					}
					continue
				}
			}
			continue NextRel
		}

		if !(r.Tags["type"] == "boundary" || r.Tags["type"] == "multipolygon") {
			continue NextRel
		}

		err := rw.osmCache.Ways.FillMembers(r.Members)
		if err != nil {
			if err != cache.NotFound {
				log.Warn(err)
			}
			continue NextRel
		}
		var admin_centre[]string
		var subareas[]string
		for _, m := range r.Members {
			if m.Role == "admin_centre" && m.Type == 0 {
				admin_centre = append(admin_centre, strconv.FormatInt(m.Id, 10))
			}
			if m.Role == "subarea" && (m.Type == 1 || m.Type == 2) {
				var id int64
				if m.Type == 2 {
					id = m.Id * -1
				} else {
					id = m.Id
				}
				subareas = append(subareas, strconv.FormatInt(id, 10))
			}
			if m.Way == nil {
				continue
			}
			err := rw.osmCache.Coords.FillWay(m.Way)
			if err != nil {
				if err != cache.NotFound {
					log.Warn(err)
				}
				continue NextRel
			}
			rw.NodesToSrid(m.Way.Nodes)
		}
		if len(admin_centre) > 0 {
			r.Tags["admin_centre"] = strings.Join(admin_centre, ", ")
		}
		if len(subareas) > 0 {
			r.Tags["subareas"] = strings.Join(subareas, ", ")
		}

		// BuildRelation updates r.Members but we need all of them
		// for the diffCache
		allMembers := r.Members

		// prepare relation first (build rings and compute actual
		// relation tags)
		prepedRel, err := geom.PrepareRelation(r, rw.srid, rw.maxGap)
		if err != nil {
			if errl, ok := err.(ErrorLevel); !ok || errl.Level() > 0 {
				log.Warn(err)
			}
			continue NextRel
		}

		// check for matches befor building the geometry
		matches := rw.polygonMatcher.MatchRelation(r)
		if len(matches) == 0 {
			continue NextRel
		}

		// build the multipolygon
		r, err = prepedRel.Build()
		if err != nil {
			if r.Geom != nil && r.Geom.Geom != nil {
				geos.Destroy(r.Geom.Geom)
			}
			if errl, ok := err.(ErrorLevel); !ok || errl.Level() > 0 {
				log.Warn(err)
			}
			continue NextRel
		}

		if rw.limiter != nil {
			start := time.Now()
			parts, err := rw.limiter.Clip(r.Geom.Geom)
			if err != nil {
				log.Warn(err)
				continue NextRel
			}
			if duration := time.Now().Sub(start); duration > time.Minute {
				log.Warnf("clipping relation %d to -limitto took %s", r.Id, duration)
			}
			for _, g := range parts {
				rel := element.Relation(*r)
				rel.Id = rw.relId(r.Id)
				rel.Geom = &element.Geometry{Geom: g, Wkb: geos.AsEwkbHex(g)}
				err := rw.inserter.InsertPolygon(rel.OSMElem, matches)
				if err != nil {
					if errl, ok := err.(ErrorLevel); !ok || errl.Level() > 0 {
						log.Warn(err)
					}
					continue
				}
			}
		} else {
			rel := element.Relation(*r)
			rel.Id = rw.relId(r.Id)
			err := rw.inserter.InsertPolygon(rel.OSMElem, matches)
			if err != nil {
				if errl, ok := err.(ErrorLevel); !ok || errl.Level() > 0 {
					log.Warn(err)
				}
				continue
			}
		}

		for _, m := range mapping.SelectRelationPolygons(rw.polygonMatcher, r) {
			err = rw.osmCache.InsertedWays.PutWay(m.Way)
			if err != nil {
				log.Warn(err)
			}
		}
		if rw.diffCache != nil {
			rw.diffCache.Ways.AddFromMembers(r.Id, allMembers)
			for _, member := range allMembers {
				if member.Way != nil {
					rw.diffCache.Coords.AddFromWay(member.Way)
				}
			}
		}
		if rw.expireor != nil {
			for _, m := range allMembers {
				if m.Way != nil {
					expire.ExpireNodes(rw.expireor, m.Way.Nodes)
				}
			}
		}
		geos.Destroy(r.Geom.Geom)
	}
	rw.wg.Done()
}

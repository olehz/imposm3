package writer

import (
	"goposm/cache"
	"goposm/element"
	"goposm/geom"
	"goposm/geom/geos"
	"goposm/mapping"
	"goposm/proj"
	"goposm/stats"
	"log"
	"runtime"
	"sync"
)

type WayWriter struct {
	osmCache             *cache.OSMCache
	ways                 chan *element.Way
	lineStringTagMatcher *mapping.TagMatcher
	polygonTagMatcher    *mapping.TagMatcher
	progress             *stats.Statistics
	insertBuffer         *InsertBuffer
	wg                   *sync.WaitGroup
}

func NewWayWriter(osmCache *cache.OSMCache, ways chan *element.Way,
	insertBuffer *InsertBuffer, lineStringTagMatcher *mapping.TagMatcher,
	polygonTagMatcher *mapping.TagMatcher, progress *stats.Statistics) *WayWriter {
	ww := WayWriter{
		osmCache:             osmCache,
		ways:                 ways,
		insertBuffer:         insertBuffer,
		lineStringTagMatcher: lineStringTagMatcher,
		polygonTagMatcher:    polygonTagMatcher,
		progress:             progress,
		wg:                   &sync.WaitGroup{},
	}

	for i := 0; i < runtime.NumCPU(); i++ {
		ww.wg.Add(1)
		go ww.loop()
	}
	return &ww
}

func (ww *WayWriter) Close() {
	ww.wg.Wait()
}

func (ww *WayWriter) loop() {
	geos := geos.NewGEOS()
	defer geos.Finish()
	for w := range ww.ways {
		ww.progress.AddWays(1)
		inserted, err := ww.osmCache.InsertedWays.IsInserted(w.Id)
		if err != nil {
			log.Println(err)
			continue
		}
		if inserted {
			continue
		}

		err = ww.osmCache.Coords.FillWay(w)
		if err != nil {
			continue
		}
		proj.NodesToMerc(w.Nodes)
		if matches := ww.lineStringTagMatcher.Match(&w.OSMElem); len(matches) > 0 {
			// make copy to avoid interference with polygon matches
			way := element.Way(*w)
			way.Geom, err = geom.LineStringWKB(geos, way.Nodes)
			if err != nil {
				if err, ok := err.(ErrorLevel); ok {
					if err.Level() <= 0 {
						continue
					}
				}
				log.Println(err)
				continue
			}
			for _, match := range matches {
				row := match.Row(&way.OSMElem)
				ww.insertBuffer.Insert(match.Table, row)
			}

		}
		if w.IsClosed() {
			if matches := ww.polygonTagMatcher.Match(&w.OSMElem); len(matches) > 0 {
				way := element.Way(*w)
				way.Geom, err = geom.PolygonWKB(geos, way.Nodes)
				if err != nil {
					if err, ok := err.(ErrorLevel); ok {
						if err.Level() <= 0 {
							continue
						}
					}
					log.Println(err)
					continue
				}
				for _, match := range matches {
					row := match.Row(&way.OSMElem)
					ww.insertBuffer.Insert(match.Table, row)
				}
			}
		}

		// if *diff {
		// 	ww.diffCache.Coords.AddFromWay(w)
		// }
	}
	ww.wg.Done()
}
Imposm 3
========

Imposm is an importer for OpenStreetMap data. It reads PBF files and
imports the data into PostgreSQL/PostGIS. It can also update the
DB from diff files.

It is designed to create databases that are optimized for rendering (i.e. generating tiles or for WMS services).

Imposm 3 is written in Go and it is a complete rewrite of the previous Python implementation.
Configurations/mappings and cache files are not compatible with Imposm 2, but they share a similar architecture.

The development of Imposm 3 was sponsored by [Omniscale](http://omniscale.com/) and development will continue as resources permit.
Please get in touch if you need commercial support or if you need specific features.


Features
--------

* High-performance
* Diff support
* Custom database schemas
* Generalized geometries


### In detail


- High performance:
  Parallel from the ground up. It distributes parsing and processing to all available CPU cores.

- Custom database schemas:
  Creates tables for different data types. This allows easier styling and better performance for rendering in WMS or tile services.

- Unify values:
  For example, the boolean values `1`, `on`, `true` and `yes` all become ``TRUE``.

- Filter by tags and values:
  Only import data you are going to render/use.

- Efficient nodes cache:
  It is necessary to store all nodes to build ways and relations. Imposm uses a file-based key-value database to cache this data.

- Generalized tables:
  Automatically creates tables with lower spatial resolutions, perfect for rendering large road networks in low resolutions.

- Limit to polygons:
  Limit imported geometries to polygons from Shapefiles or GeoJSON, for city/state/country imports.

- Easy deployment:
  Single binary with only runtime dependencies to common libs (GEOS, SQLite and LevelDB)

- Support for table namespace (PostgreSQL schema)


Performance
-----------

Imposm 3 is much faster than Imposm 2 and osm2pgsql:

* Makes full use of all available CPU cores
* Bulk inserts into PostgreSQL with `COPY FROM`
* Efficient intermediate cache for reduced IO load during ways and relations building


Some import times from a Hetzner EX 4S server (Intel i7-2600 CPU @ 3.40GHz, 32GB RAM and 2TB software RAID1 (2x2TB 7200rpm SATA disks)) for imports of a 20.5GB planet PBF (2013-06-14) with generalized tables:

* 6:30h in normal-mode
* 13h in diff-mode

osm2pgsql required between 2-8 days in a [similar benchmark (slide 7)](http://www.geofabrik.de/media/2012-09-08-osm2pgsql-performance.pdf) with a smaller planet PBF file (~15GB).

Benchmarks with SSD are TBD.

Import of Europe 11GB PBF with generalized tables:

* 2:20h in normal-mode


Current status
--------------

Imposm 3 is used in production but there is no official release yet.

### Missing ###

Compared to Imposm 2:

* Support for other projections than EPSG:3857 or EPSG:4326
* Import of XML files (unlikely to be implemented in the future, use [osmosis](http://wiki.openstreetmap.org/wiki/Osmosis) to convert XML to PBF first)
* Custom field/filter functions

Installation
------------

### Binary

There are no official releases, but you find development builds at <http://imposm.org/static/rel/>.
These builds are for x86 64bit Linux and require *no* further dependencies. Download, untar and start `imposm3`.
(Note: These binaries require glibc >= 2.15 at the moment. Ubuntu 12.04 is recent enough, Debian 7 not.)

### Source

There are some dependencies:

#### Compiler

You need [Go >=1.1](http://golang.org).

#### C/C++ libraries

Other dependencies are [libleveldb][], [libgeos][] and [protobuf][].
Imposm 3 was tested with recent versions of these libraries, but you might succeed with older versions.
GEOS >=3.2 is recommended, since it became much more robust when handling invalid geometries.
For best performance use [HyperLevelDB][libhyperleveldb] as an in-place replacement for libleveldb.


[libleveldb]: https://code.google.com/p/leveldb/
[libhyperleveldb]: https://github.com/rescrv/HyperLevelDB
[libgeos]: http://trac.osgeo.org/geos/
[protobuf]: https://code.google.com/p/protobuf/

#### Go libraries

Imposm3 uses the following libraries.

- <https://github.com/jmhodges/levigo>
- <https://github.com/golang/protobuf/proto>
- <https://github.com/golang/protobuf/protoc-gen-go>
- <https://github.com/lib/pq>

`go get` will fetch these, but you can also use [godep][] to use a provided (vendorized) set of these dependencies.

[godep]: https://github.com/tools/godep


#### Other

Fetching Imposm and the Go libraries requires [mercurial][] and [git][].

[mercurial]: http://mercurial.selenic.com/
[git]: http://git-scm.com/


#### Compile

Create a new [Go workspace](http://golang.org/doc/code.html):

    mkdir imposm
    cd imposm
    export GOPATH=`pwd`

Get Imposm 3 and all dependencies:

    go get github.com/olehz/imposm3
    go install github.com/olehz/imposm3

Done. You should now have an imposm3 binary in `$GOPATH/bin`.

Go compiles to static binaries and so Imposm 3 has no runtime dependencies to Go.
Just copy the `imposm3` binary to your server for deployment. The C/C++ libraries listed above are still required though.

##### Godep

Imposm contains a fixed set of the dependencies that are known to work. You need to install Imposm with [godep][] to compile with this set.

    git clone https://github.com/omniscale/imposm3 src/github.com/omniscale/imposm3
    cd src/github.com/omniscale/imposm3
    godep go install ./...

### FreeBSD

On FreeBSD you can use the ports system: Simply fetch https://github.com/thomersch/imposm3-freebsd and run `make install`.

Usage
-----

`imposm3` has multiple subcommands. Use `imposm3 import` for basic imports.

For a simple import:

    imposm3 import -connection postgis://user:password@host/database \
        -mapping mapping.json -read /path/to/osm.pbf -write

You need a JSON file with the target database mapping. See `example-mapping.json` to get an idea what is possible with the mapping.

Imposm creates all new tables inside the `import` table schema. So you'll have `import.osm_roads` etc. You can change the tables to the `public` schema:

    imposm3 import -connection postgis://user:passwd@host/database \
        -mapping mapping.json -deployproduction


You can write some options into a JSON configuration file:

    {
        "cachedir": "/var/local/imposm3",
        "mapping": "mapping.json",
        "connection": "postgis://user:password@localhost:port/database"
    }

To use that config:

    imposm3 import -config config.json [args...]

For more options see:

    imposm3 import -help


Note: TLS/SSL support is disabled by default due to the lack of renegotiation support in Go's TLS implementation. You can re-enable encryption by setting the `PGSSLMODE` environment variable or the `sslmode` connection option to `require` or `verify-full`, eg: `-connect postgis://host/dbname?sslmode=require`. You will need to disable renegotiation support on your server to prevent connection errors on larger imports. You can do this by setting `ssl_renegotiation_limit` to 0 in your PostgreSQL server configuration.


Documentation
-------------

The latest documentation can be found here: <http://imposm.org/docs/imposm3/latest/>

Support
-------

There is a [mailing list at Google Groups](http://groups.google.com/group/imposm) for all questions. You can subscribe by sending an email to: `imposm+subscribe@googlegroups.com`

For commercial support [contact Omniscale](http://omniscale.com/contact).

Development
-----------

The source code is available at: <https://github.com/omniscale/imposm3/>

You can report any issues at: <https://github.com/omniscale/imposm3/issues>

License
-------

Imposm 3 is released as open source under the Apache License 2.0. See LICENSE.

All dependencies included as source code are released under a BSD-ish license except the YAML package.
The YAML package is released as LGPL3 with an exception that permits static linking. See LICENSE.deps.

All dependencies included in binary releases are released under a BSD-ish license except the GEOS package.
The GEOS package is released as LGPL3 and is linked dynamically. See LICENSE.bin.


### Test ###

#### Unit tests ####

    go test imposm3/...


#### System tests ####

There are system test that import and update OSM data and verify the database content.

##### Dependencies #####

These tests are written in Python and requires `nose`, `shapely` and `psycopg2`.

On a recent Ubuntu can install the following packages for that: `python-nose python-shapely python-psycopg2`
Or you can [install a Python virtualenv](https://virtualenv.pypa.io/en/latest/installation.html):

    virtualenv imposm3test
    source imposm3test/bin/activate
    pip install nose shapely psycopg2

You also need `osmosis` to create test PBF files.
There is a Makefile that (re)builds `imposm3` and creates all test files if necessary and then runs the test itself.

    make test

Call `make test-system` to skip the unit tests.

WARNING: It uses your local PostgeSQL database (`import` schema). Change the database with the standard `PGDATABASE`, `PGHOST`, etc. environment variables.

# v 0.4.0 (2019-04-14)

## New features

* added command line parameters -v, -vv, -vvv for info,debug,trace  log levels 
* added command line -chunk as replacement/override for  data-chuck-duration parameter in the config file (implement #10)
* added config  rw-max-retries,  rw-retry-delay  to fix minor errors (timeouts, latency, net glich)
* added config  num-workers to add concurrent read/write on the same db and chuck time ( several measurements at timel )

## fixes

* fixes for #6,#11,#9,#8

# v 0.3.0 (2019-04-11)

## New features

* added -full option to the -action copy/fullcopy execution modes

## fixes

* fixes for #1,#4,#5,#7

# v 0.2.0 (2019-04-11)

## New features

* added replicashema and fullcopy execution mode.
* added /queryactive endpoint available to external tools.
* added syncronization tunning params data-chuck-duration , max-retention-interval  


# v 0.1.0 (2019-04-07)

## New features

* first release with  slavei db  syncronization with master
* Added initial db schema replication 
* Added initial db data replication
* Added action copy


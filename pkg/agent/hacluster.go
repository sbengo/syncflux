package agent

import (
	"regexp"
	"sync"
	"time"
)

type InfluxSchDb struct {
	Name   string
	DefRp  string
	Rps    []*RetPol
	Ftypes map[string]map[string]string
}

type HACluster struct {
	Master                     *InfluxMonitor
	Slave                      *InfluxMonitor
	CheckInterval              time.Duration
	ClusterState               string
	SlaveStateOK               bool
	SlaveLastOK                time.Time
	SlaveCheckDuration         time.Duration
	MasterStateOK              bool
	MasterLastOK               time.Time
	MasterCheckDuration        time.Duration
	ClusterNumRecovers         int
	ClusterLastRecoverDuration time.Duration
	statsData                  sync.RWMutex
	Schema                     []*InfluxSchDb
	ChunkDuration              time.Duration
	MaxRetentionInterval       time.Duration
}

type ClusterStatus struct {
	ClusterState               string
	ClusterNumRecovers         int
	ClusterLastRecoverDuration time.Duration
	MID                        string
	SID                        string
	MasterState                bool
	MasterLastOK               time.Time
	SlaveState                 bool
	SlaveLastOK                time.Time
}

func (hac *HACluster) GetStatus() *ClusterStatus {
	hac.statsData.RLock()
	defer hac.statsData.RUnlock()
	return &ClusterStatus{
		ClusterState:               hac.ClusterState,
		MID:                        hac.Master.cfg.Name,
		SID:                        hac.Slave.cfg.Name,
		ClusterNumRecovers:         hac.ClusterNumRecovers,
		ClusterLastRecoverDuration: hac.ClusterLastRecoverDuration,
		MasterState:                hac.MasterStateOK,
		MasterLastOK:               hac.MasterLastOK,
		SlaveState:                 hac.SlaveStateOK,
		SlaveLastOK:                hac.SlaveLastOK,
	}
}

// From Master to Slave
func (hac *HACluster) GetSchema(dbfilter string) ([]*InfluxSchDb, error) {

	schema := []*InfluxSchDb{}
	var filter *regexp.Regexp
	var err error

	if len(dbfilter) > 0 {
		filter, err = regexp.Compile(dbfilter)
		if err != nil {
			return nil, err
		}
	}

	srcDBs, _ := GetDataBases(hac.Master.cli)

	for _, db := range srcDBs {

		if len(dbfilter) > 0 && !filter.MatchString(db) {
			log.Debugf("Database %s not match to regex %s:  skipping.. ", db, dbfilter)
			continue
		}

		// Get Retention policies
		rps, err := GetRetentionPolicies(hac.Master.cli, db)
		if err != nil {
			log.Errorf("Error on get Retention Policies on Database %s MasterDB %s : Error: %s", db, hac.Master.cfg.Name, err)
			continue
		}

		//check for default RP
		var defaultRp *RetPol
		for _, rp := range rps {
			if rp.Def {
				defaultRp = rp
				break
			}
		}

		// Check if default RP is valid
		if defaultRp == nil {
			log.Errorf("Error on Create DB  %s on SlaveDB %s : Database has not default Retention Policy ", db, hac.Slave.cfg.Name)
			continue
		}

		meas := GetMeasurements(hac.Master.cli, db)

		mf := make(map[string]map[string]string, len(meas))

		for _, m := range meas {
			log.Debugf("discovered measurement  %s on DB: %s", m, db)
			mf[m] = GetFields(hac.Master.cli, db, m)
		}
		//
		schema = append(schema, &InfluxSchDb{Name: db, DefRp: defaultRp.Name, Rps: rps, Ftypes: mf})
	}
	hac.Schema = schema
	return schema, nil
}

// From Master to Slave
func (hac *HACluster) ReplicateSchema(schema []*InfluxSchDb) error {

	for _, db := range schema {
		//check for default RP
		var defaultRp *RetPol
		for _, rp := range db.Rps {
			if rp.Def {
				defaultRp = rp
				break
			}
		}
		crdberr := CreateDB(hac.Slave.cli, db.Name, defaultRp)
		if crdberr != nil {
			log.Errorf("Error on Create DB  %s on SlaveDB %s : Error: %s", db, hac.Slave.cfg.Name, crdberr)
			continue
		}
		for _, rp := range db.Rps {
			if rp.Def {
				// default has been previously created
				continue
			}
			log.Infof("Creating Extra Retention Policy %s on database %s ", rp.Name, db)
			crrperr := CreateRP(hac.Slave.cli, db.Name, rp)
			if crrperr != nil {
				log.Errorf("Error on Create Retention Policies on Database %s SlaveDB %s : Error: %s", db, hac.Slave.cfg.Name, crrperr)
				continue
			}
			log.Infof("Replication Schema: DB %s OK", db)

		}
	}
	return nil
}

func (hac *HACluster) ReplicateData(schema []*InfluxSchDb, start time.Time, end time.Time) error {
	for _, db := range schema {
		for _, rp := range db.Rps {
			log.Infof("Replicating Data from DB %s RP %s...", db.Name, rp.Name)
			//log.Debugf("%s RP %s... SCHEMA %#+v.", db.Name, rp.Name, db)
			err := SyncDBRP(hac.Master, hac.Slave, db.Name, rp, start, end, db, hac.ChunkDuration, hac.MaxRetentionInterval)
			if err != nil {
				log.Errorf("Data Replication error in DB [%s] RP [%s] | Error: %s", db, rp.Name, err)
			}
		}
	}
	return nil
}

func (hac *HACluster) ReplicateDataFull(schema []*InfluxSchDb) error {
	for _, db := range schema {
		for _, rp := range db.Rps {
			log.Infof("Replicating Data from DB %s RP %s....", db.Name, rp.Name)
			start, end := rp.GetFirstLastTime(hac.MaxRetentionInterval)
			err := SyncDBRP(hac.Master, hac.Slave, db.Name, rp, start, end, db, hac.ChunkDuration, hac.MaxRetentionInterval)
			//err := SyncDBFull(hac.Master, hac.Slave, db.Name, rp, db, hac.ChunkDuration, hac.MaxRetentionInterval)
			if err != nil {
				log.Errorf("Data Replication error in DB [%s] RP [%s] | Error: %s", db, rp.Name, err)
			}
		}
	}
	return nil
}

// ScltartMonitor Main GoRutine method to begin snmp data collecting
func (hac *HACluster) SuperVisor(wg *sync.WaitGroup) {
	wg.Add(1)
	go hac.startSupervisorGo(wg)
}

// OK -> CHECK_SLAVE_DOWN -> RECOVERING -> OK

func (hac *HACluster) checkCluster() {

	//check Master

	lastMaster, lastmaOK, durationM := hac.Master.GetState()
	lastSlave, lastslOK, durationS := hac.Slave.GetState()

	// STATES
	// ALL OK
	// DETECTED SLAVE DONW
	// STILL DOWN
	// DETECTED UP
	// RECOVERING

	log.Info("HACluster check....")

	switch {
	//detected Down
	case hac.SlaveStateOK && lastSlave != true:
		log.Infof("HACLuster: detected DOWN Last(%s) Duratio OK (%s)", lastslOK.String(), durationS.String())
		hac.statsData.Lock()
		hac.ClusterState = "CHECK_SLAVE_DOWN"
		hac.MasterStateOK = lastMaster
		hac.MasterLastOK = lastmaOK
		hac.MasterCheckDuration = durationM
		hac.SlaveLastOK = lastslOK
		hac.SlaveStateOK = lastSlave
		hac.SlaveCheckDuration = durationS
		hac.statsData.Unlock()
		return
	//still Down
	case hac.ClusterState == "CHECK_SLAVE_DOWN" && lastSlave == false:
		hac.statsData.Lock()
		hac.MasterStateOK = lastMaster
		hac.MasterLastOK = lastmaOK
		hac.MasterCheckDuration = durationM
		hac.statsData.Unlock()
		return
	// Detected UP
	case hac.ClusterState == "CHECK_SLAVE_DOWN" && lastSlave == true:
		log.Infof("HACLuster: detected UP Last(%s) Duratio OK (%s) RECOVERING", lastslOK.String(), durationS.String())
		// service has been recovered is time to sincronize

		hac.statsData.Lock()
		startTime := hac.SlaveLastOK.Add(-hac.CheckInterval)

		hac.ClusterState = "RECOVERING"
		hac.MasterStateOK = lastMaster
		hac.MasterLastOK = lastmaOK
		hac.MasterCheckDuration = durationM
		hac.SlaveLastOK = lastslOK
		hac.SlaveStateOK = lastSlave
		hac.SlaveCheckDuration = durationS
		hac.statsData.Unlock()

		endTime := lastslOK

		// after conection recover with de database the
		// the client should be updated before any connection test
		hac.Slave.UpdateCli()
		// begin recover
		log.Infof("HACLUSTER: INIT RECOVERY : FROM [ %s ] TO [ %s ]", startTime.String(), endTime.String())
		start := time.Now()
		hac.ReplicateData(hac.Schema, startTime, endTime)
		elapsed := time.Since(start)
		log.Infof("HACLUSTER: DATA SYNCRONIZATION Took %s", elapsed.String())

		hac.statsData.Lock()
		hac.ClusterState = "OK"
		hac.ClusterNumRecovers++
		hac.ClusterLastRecoverDuration = elapsed
		hac.statsData.Unlock()
		return
	//Detected Recover
	case hac.ClusterState == "RECOVERING":
		log.Infof("HACluster: Database Still recovering")
		hac.statsData.Lock()
		hac.MasterStateOK = lastMaster
		hac.MasterLastOK = lastmaOK
		hac.MasterCheckDuration = durationM
		hac.SlaveLastOK = lastslOK
		hac.SlaveStateOK = lastSlave
		hac.SlaveCheckDuration = durationS
		hac.statsData.Unlock()
		return
	case hac.ClusterState == "OK" && lastSlave == true:
		hac.statsData.Lock()
		hac.MasterStateOK = lastMaster
		hac.MasterLastOK = lastmaOK
		hac.MasterCheckDuration = durationM
		hac.SlaveLastOK = lastslOK
		hac.SlaveStateOK = lastSlave
		hac.SlaveCheckDuration = durationS
		hac.statsData.Unlock()
	default:
		log.Warnf("HACLUSTER: undhanled State Last MasterOK %t %s", lastMaster, lastmaOK.String())
		log.Warnf("HACLUSTER: undhanled State Last SlaveOK %t %s", lastSlave, lastslOK.String())
		return
	}

}

func (hac *HACluster) startSupervisorGo(wg *sync.WaitGroup) {
	defer wg.Done()

	log.Infof("Beginning Supervision process  process each %s ", hac.CheckInterval.String())
	hac.MasterStateOK, hac.MasterLastOK, _ = hac.Master.GetState()
	hac.SlaveStateOK, hac.SlaveLastOK, _ = hac.Slave.GetState()

	t := time.NewTicker(hac.CheckInterval)
	for {
		hac.checkCluster()
	LOOP:
		for {
			select {
			case <-t.C:
				break LOOP
			}
		}
	}
}

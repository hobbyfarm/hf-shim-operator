package utils

import (
	"encoding/base64"

	"gopkg.in/yaml.v2"
)

type sshAuthKeys []string

func MergeCloudInit(pubKey string, cloudInit string) (mergedCloudInit string, err error) {

	cloudInitMap := make(map[interface{}]interface{})
	var sshKeyList []interface{}
	pubKeyInterface := []interface{}{pubKey}
	if len(cloudInit) > 0 {
		decodedCloudInit, err := base64.StdEncoding.DecodeString(cloudInit)
		err = yaml.Unmarshal(decodedCloudInit, &cloudInitMap)
		if err != nil {
			return mergedCloudInit, err
		}
		if sshKeys, ok := cloudInitMap["ssh_authorized_keys"]; ok {
			sshKeyList = sshKeys.([]interface{})
		}
	}

	sshKeyList = append(sshKeyList, pubKeyInterface...)

	cloudInitMap["ssh_authorized_keys"] = sshKeyList
	mergedCloutInitByte, err := yaml.Marshal(cloudInitMap)
	if err != nil {
		return mergedCloudInit, err
	}

	mergedCloudInit = base64.StdEncoding.EncodeToString(mergedCloutInitByte)

	return mergedCloudInit, nil
}

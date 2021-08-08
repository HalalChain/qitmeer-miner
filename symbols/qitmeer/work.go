// Copyright (c) 2019 The qitmeer developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.
package qitmeer

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Qitmeer/qitmeer-miner/common"
	"github.com/Qitmeer/qitmeer-miner/core"
	"github.com/Qitmeer/qitmeer/core/types/pow"
	"math/big"
	"strings"
	"time"
)

type getResponseJson struct {
	Result  BlockHeader
	Id      int    `json:"id"`
	Error   string `json:"error"`
	JsonRpc string `json:"jsonrpc"`
}

var ErrSameWork = fmt.Errorf("Same work, Had Submitted!")

type getSubmitResponseJson struct {
	Result  string `json:"result"`
	Id      int    `json:"id"`
	Error   string `json:"error"`
	JsonRpc string `json:"jsonrpc"`
}
type QitmeerWork struct {
	core.Work
	Block       *BlockHeader
	PoolWork    NotifyWork
	stra        *QitmeerStratum
	StartWork   bool
	ForceUpdate bool
}

func (this *QitmeerWork) CopyNew() QitmeerWork {
	newWork := QitmeerWork{}
	newWork.Cfg = this.Cfg
	if this.Cfg.PoolConfig.Pool != "" {
		//pool work
		newWork.stra = this.stra
		b, _ := json.Marshal(this.stra.PoolWork)
		var pw NotifyWork
		_ = json.Unmarshal(b, &pw)
		newWork.PoolWork = pw

	} else {
		newWork.Rpc = this.Rpc
		newWork.StartWork = this.StartWork
		b, _ := json.Marshal(this.Block)
		var w BlockHeader
		_ = json.Unmarshal(b, &w)
		newWork.Block = &w
		newWork.Block.SetTxs(this.Block.transactions)
		newWork.Block.Pow = this.Block.Pow
		newWork.Block.NodeInfo = this.Block.NodeInfo
		newWork.Block.ParentRoot = this.Block.ParentRoot
		newWork.Block.Parents = this.Block.Parents
		newWork.Block.Transactions = this.Block.Transactions
		newWork.Block.StateRoot = this.Block.StateRoot
		newWork.ForceUpdate = this.ForceUpdate
		newWork.Block.Height = this.Block.Height
	}

	return newWork
}

func (this *QitmeerWork) GetPowType() pow.PowType {
	switch this.Cfg.NecessaryConfig.Pow {
	case POW_MEER_CRYPTO:
		return pow.MEERXKECCAKV1
	default:
		return pow.BLAKE2BD
	}
}

//GetBlockTemplate
func (this *QitmeerWork) Get() bool {
	this.ForceUpdate = false
	body := this.Rpc.RpcResult("getBlockTemplate", []interface{}{[]string{}, this.GetPowType()})
	if body == nil {
		if this.Cfg.OptionConfig.TaskForceStop {
			this.ForceUpdate = true
		}
		return false
	}
	var blockTemplate getResponseJson
	err := json.Unmarshal(body, &blockTemplate)
	if err != nil {
		var r map[string]interface{}
		_ = json.Unmarshal(body, &r)
		if strings.Contains(string(body), "download") {
			common.MinerLoger.Warn(fmt.Sprintf("[getBlockTemplate warn] %s ", string(body)))
		} else {
			common.MinerLoger.Debug("[getBlockTemplate error]", "result", string(body))
		}
		if this.Cfg.OptionConfig.TaskForceStop {
			this.ForceUpdate = true
		}
		return false
	}
	if this.Block != nil && this.Block.Height == blockTemplate.Result.Height &&
		(time.Now().Unix()-this.GetWorkTime) < int64(this.Cfg.OptionConfig.Timeout)*10 {
		//not has new work
		return false
	}

	target := ""
	n := new(big.Int)
	switch this.Cfg.NecessaryConfig.Pow {
	case POW_MEER_CRYPTO:
		blockTemplate.Result.Pow = pow.GetInstance(pow.MEERXKECCAKV1, 0, []byte{})
		target = blockTemplate.Result.PowDiffReference.Target
		n, _ = n.SetString(target, 16)
		blockTemplate.Result.Difficulty = uint64(pow.BigToCompact(n))
		blockTemplate.Result.Target = target
	}

	blockTemplate.Result.HasCoinbasePack = false
	_, _ = blockTemplate.Result.CalcCoinBase(this.Cfg, this.Cfg.SoloConfig.RandStr, uint64(0), this.Cfg.SoloConfig.MinerAddr)
	blockTemplate.Result.BuildMerkleTreeStore(0)
	this.Block = &blockTemplate.Result
	this.Started = uint32(time.Now().Unix())
	this.GetWorkTime = time.Now().Unix()
	common.CurrentHeight = this.Block.Height
	this.Cfg.OptionConfig.Target = this.Block.Target
	common.MinerLoger.Info(fmt.Sprintf("getBlockTemplate height:%d , target :%s", this.Block.Height, target))
	return true
}

//Submit
func (this *QitmeerWork) Submit(subm string) error {
	this.Lock()
	defer this.Unlock()
	if this.LastSub == subm {
		return ErrSameWork
	}
	this.LastSub = subm
	var body []byte
	var res getSubmitResponseJson
	startTime := time.Now().Unix()
	for {
		select {
		case <-this.Quit.Done():
			common.MinerLoger.Debug("exit submit")
			return nil
		default:

		}
		// if the reason of submit error is network failed
		// to keep the work
		// then retry submit
		body = this.Rpc.RpcResult("submitBlock", []interface{}{subm})
		err := json.Unmarshal(body, &res)
		if err != nil {
			// 2min timeout
			if time.Now().Unix()-startTime >= 120 {
				break
			}
			common.MinerLoger.Error(fmt.Sprintf("[submit error]" + string(body) + err.Error()))
			common.Usleep(1)
			continue
		}
		break
	}

	if !strings.Contains(res.Result, "Block submitted accepted") {
		common.MinerLoger.Error("[submit error] " + string(body))
		if strings.Contains(res.Result, "The tips of block is expired") {
			return ErrSameWork
		}
		return errors.New("[submit data failed]" + res.Result)
	}
	return nil
}

// pool get work
func (this *QitmeerWork) PoolGet() bool {
	if !this.stra.PoolWork.NewWork {
		return false
	}
	err := this.stra.PoolWork.PrepWork()
	if err != nil {
		common.MinerLoger.Error(err.Error())
		return false
	}

	if (this.stra.PoolWork.JobID != "" && this.stra.PoolWork.Clean) || this.PoolWork.JobID != this.stra.PoolWork.JobID {
		this.stra.PoolWork.Clean = false
		this.Cfg.OptionConfig.Target = fmt.Sprintf("%064x", common.BlockBitsToTarget(this.stra.PoolWork.Nbits, 2))
		this.PoolWork = this.stra.PoolWork
		common.CurrentHeight = uint64(this.stra.PoolWork.Height)
		common.JobID = this.stra.PoolWork.JobID
		return true
	}

	return false
}

//pool submit work
func (this *QitmeerWork) PoolSubmit(subm string) error {
	if this.LastSub == subm {
		return ErrSameWork
	}
	this.LastSub = subm
	arr := strings.Split(subm, "-")
	data, err := hex.DecodeString(arr[0])
	if err != nil {
		return err
	}
	sub, err := this.stra.PrepSubmit(data, arr[1], arr[2])
	if err != nil {
		return err
	}
	m, err := json.Marshal(sub)
	if err != nil {
		return err
	}
	_, err = this.stra.Conn.Write(m)
	if err != nil {
		common.MinerLoger.Debug("[submit error][pool connect error]", "error", err)
		return err
	}
	_, err = this.stra.Conn.Write([]byte("\n"))
	if err != nil {
		common.MinerLoger.Debug(err.Error())
		return err
	}

	return nil
}

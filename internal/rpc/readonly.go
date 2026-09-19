package rpc

// ReadOnlyMethods is the reviewed allowlist used when automatic retries or
// shadow duplication are allowed. Unknown methods are treated as unsafe.
var ReadOnlyMethods = map[string]bool{
	"web3_clientVersion": true,
	"web3_sha3":          true,
	"net_version":        true,
	"net_listening":      true,
	"net_peerCount":      true,

	"eth_protocolVersion":                  true,
	"eth_syncing":                          true,
	"eth_chainId":                          true,
	"eth_blockNumber":                      true,
	"eth_coinbase":                         true,
	"eth_mining":                           true,
	"eth_hashrate":                         true,
	"eth_gasPrice":                         true,
	"eth_blobBaseFee":                      true,
	"eth_maxPriorityFeePerGas":             true,
	"eth_feeHistory":                       true,
	"eth_getBalance":                       true,
	"eth_getStorageAt":                     true,
	"eth_getTransactionCount":              true,
	"eth_getBlockTransactionCountByHash":   true,
	"eth_getBlockTransactionCountByNumber": true,
	"eth_getUncleCountByBlockHash":         true,
	"eth_getUncleCountByBlockNumber":       true,
	"eth_getCode":                          true,
	"eth_call":                             true,
	"eth_estimateGas":                      true,
	"eth_getBlockByHash":                   true,
	"eth_getBlockByNumber":                 true,
	"eth_getTransactionByHash":             true,
	"eth_getTransactionReceipt":            true,
	"eth_getUncleByBlockHashAndIndex":      true,
	"eth_getUncleByBlockNumberAndIndex":    true,
	"eth_getLogs":                          true,
	"eth_getProof":                         true,
	"eth_getBlockReceipts":                 true,
	"eth_getRawTransactionByHash":          true,
	"eth_getRawTransactionFromBlock":       true,
}

func IsReadOnlyMethod(method string) bool {
	return ReadOnlyMethods[method]
}

package main

import(
	"fmt"
	"io"
	"net/http"
	"runtime"
	"log"
	"time"
	"crypto/md5"
	"encoding/hex"
	"strings"
	"strconv"
	"github.com/garyburd/redigo/redis"//https://gowalker.org/github.com/garyburd/redigo/redis#Script_Do manul
	//"os"
	//"reflect"
	"encoding/json"
	"errors"
)

//使用redis连接池

func newPool()  *redis.Pool{
	return &redis.Pool{
		MaxIdle: 80,
		MaxActive: 1200,
		Dial: func() (redis.Conn, error){
			c, err := redis.Dial("tcp","127.0.0.1:6379")
			if err != nil{
				panic(err.Error())
			}
			return c, err
		},
	}
}

var pool = newPool()


//platform key and plat config
var keyMap = map[string]string{
	"LeShop":"123qwe321",
}

//type timesLimit ［4][2]int32

var timesConfig = map[string]map[string][3]int32{
	//"LeShop" : {t1:[20, 20], t2:[40, 40], t3:[80, 80], [160, 160]}//time,times
	"LeShop" : {"0":{60, 2, 60}, "1":{120, 40, 120}, "2":{240, 80, 240}, "3":{480, 160, 480}},//时间段，次数限制, 锁多久
}


//request param 
type params struct{
	uid 	string
	uip 	string
	plat 	string
}

var requestParam params

//init user redis data
type redisData struct{
	Time 		int64
	Times   	int32
	IsLock  	bool
	LockTime	int32
	LockStart 	int64
}

var initData = [4]redisData{
		{Time:time.Now().Unix(), Times:1, IsLock:false, LockTime:30, LockStart:time.Now().Unix()},
		{Time:time.Now().Unix(), Times:1, IsLock:false, LockTime:60, LockStart:time.Now().Unix()},
		{Time:time.Now().Unix(), Times:1, IsLock:false, LockTime:120, LockStart:time.Now().Unix()},
		{Time:time.Now().Unix(), Times:1, IsLock:false, LockTime:240, LockStart:time.Now().Unix()},
	}

//response user data

type rUserData struct{
	IfLock 		bool
	LockTime 	int64
}
//sso 防刷流程

//redis key 
var redisKey string

func ssoMain(w http.ResponseWriter, r *http.Request){
	r.ParseForm()
	var pkey 	string
	var uid  	string
	var uip		string
	var plat	string
	var sign 	string
	//数据列表
	
	uid  = r.FormValue("uid")
	uip  = r.FormValue("ip")
	plat = r.FormValue("plat")
	sign = r.FormValue("_sign")

	var storeRedisData 	map[string]string
	var returnUserData  rUserData 	

	if _, ok := keyMap[plat];!ok{
		io.WriteString(w, "you are not plat user \n\n")
	}else{
		pkey = keyMap[plat]
	}

	if(sign != ""){
		signArr 	:= strings.Split(sign, ".")
		Rmd5str  	:= signArr[0]//请求加密串
		requestTime := signArr[1]//请求时间戳

		//验证md5字符串
		md5Ob := md5.New()
		md5Ob.Write([]byte(pkey+requestTime))
		md5str := hex.EncodeToString(md5Ob.Sum(nil))

		if md5str != Rmd5str{
			io.WriteString(w, "your param error \n\n")
		}
	}
	

	requestParam.uid  = uid
	requestParam.uip  = uip
	requestParam.plat = plat

	redisKey = requestParam.uip + "_plat_" + requestParam.plat

	uData, err := getUserSsoData()
	if err != nil{
		io.WriteString(w, err.Error())
	}

	if len(uData) == 0{
		ok, err := initUserSsoData();
		if !ok{
			io.WriteString(w, err.Error())
		}
		io.WriteString(w, "user data empty, init finished")
	}else{
		storeRedisData, returnUserData, _ = ssoHandlProcess(uData)
	}

	ok, err := setUserSsoData(storeRedisData)
	if !ok{
		io.WriteString(w, err.Error())
	}

	byteStr, err := json.Marshal(returnUserData)
	if err != nil{
		io.WriteString(w, " json encode error")
	}

	io.WriteString(w, string(byteStr))

	log.Println("time: "+fmt.Sprintf("%d",time.Now().Unix())+" URL: "+r.RequestURI)
}

//参数处理加工
func parseParam(){

}

func getUserSsoData()(map[string]string, error){
	var error error
	var redisKey = redisKey
	connection := pool.Get()
	defer connection.Close()

	userSsoData, err := connection.Do("HGETALL", redisKey)
	if err != nil{
		return nil , errors.New("get data fail")
	}


	uData, err := redis.StringMap(userSsoData, error)

	if err != nil{
		return nil, errors.New("convert get data fail")
	}

	return uData, nil
}
/*初始化数据*/
func initUserSsoData()(bool, error){

	//var error 		error
	var redisKey = redisKey

	connection := pool.Get()
	defer	connection.Close()

	if _, err := connection.Do("WATCH",redisKey,1); err != nil{
		return false, errors.New(redisKey+" key lock fail: "+err.Error())
	}

	if _, err := connection.Do("MULTI"); err != nil{
		return false, errors.New(redisKey+" key multi fail: "+err.Error())
	}

	for k, v := range initData{
		
		//vJsonStr, err := json.Marshal( {time:time.Now().Unix(), times:1, isLock:false, lockTime:30, lockStart:time.Now().Unix()} )
		v.LockTime =  timesConfig[requestParam.plat][strconv.Itoa(k)][2]

		vJsonStr, err := json.Marshal(v)

		if err != nil{
			return false, errors.New("json encode error: "+err.Error())
		}

		if _, err := connection.Do("HSET", redisKey, k, string(vJsonStr)); err != nil{
			return false, errors.New(redisKey+" init fail: "+err.Error())
		}
	}

	if _, err := connection.Do("EXEC"); err != nil{
		return false, errors.New(redisKey+"key exec fail: "+err.Error())
	}

	return true, nil
}

func setUserSsoData(storeRedisData map[string]string)(bool, error){
	//var error 		error
	var redisKey = redisKey

	connection := pool.Get()
	defer	connection.Close()

	if _, err := connection.Do("WATCH",redisKey,1); err != nil{
		return false, errors.New(redisKey+" key lock fail: "+err.Error())
	}

	if _, err := connection.Do("MULTI"); err != nil{
		return false, errors.New(redisKey+" key multi fail: "+err.Error())
	}

	for k, v := range storeRedisData{
		if _, err := connection.Do("HSET", redisKey, k, v); err != nil{
			return false, errors.New(redisKey+" init fail: "+err.Error())
		}
	}

	if _, err := connection.Do("EXEC"); err != nil{
		return false, errors.New(redisKey+"key exec fail: "+err.Error())
	}

	return true, nil
}
/*防刷处理流程*/
func ssoHandlProcess(uData map[string]string)(map[string]string, rUserData, error){
	var data   redisData
	var rData  = make(map[string]string, len(uData))
		rData  = uData

	var rUserStatus rUserData

	var errString string

	for k, v := range uData{
		err := json.Unmarshal([]byte(v), &data)
		if err != nil{
			errString = " json decode error"
		}

		if data.IsLock{//lock
			if data.LockStart + int64(data.LockTime) >= time.Now().Unix() {//锁时间未到，遍历上级锁，加次数
				
				rUserStatus.IfLock   = true
				rUserStatus.LockTime = data.LockStart + int64(data.LockTime)

				continue
			}else{//锁时间到，清除锁信息
				data.Time      = time.Now().Unix()
				data.Times     = 1
				data.IsLock    = false
				data.LockTime  = timesConfig[requestParam.plat][k][2]
				data.LockStart = time.Now().Unix()
			}
		}else{//unlock
			if time.Now().Unix() - data.Time <= int64(timesConfig[requestParam.plat][k][0]){
				data.Times = data.Times + 1
			}else{
				data.Times = 1
				data.Time  = time.Now().Unix()
			}
			
			if data.Times >= timesConfig[requestParam.plat][k][1]{//compare the now times with conig times,
				data.Time      = time.Now().Unix()
				data.IsLock    = true
				data.LockStart = time.Now().Unix()

				rUserStatus.IfLock   = true
				rUserStatus.LockTime = data.LockStart + int64(data.LockTime)		
			}
		}

		byteData, err := json.Marshal(data)
		if err != nil{
			errString = "json encode error"
		}
		rData[k] = string(byteData)
	}

	return rData, rUserStatus, errors.New(errString)
}

func checkTtimeTtimes(){

}

/*
func Struct2Map(obj interface{}) map[string]interface{} {
	t := reflect.TypeOf(obj)
	v := reflect.ValueOf(obj)

	var data = make(map[string]interface{})
	for i := 0; i < t.NumField(); i++ {
		data[t.Field(i).Name] = v.Field(i).Interface()
	}
	return data
}
*/
func main(){
	runtime.GOMAXPROCS(runtime.NumCPU())
	http.HandleFunc("/sso",ssoMain)
	http.ListenAndServe(":8000",nil)
}
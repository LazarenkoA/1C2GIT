@echo off
set SrvcName="1C2GIT_2"
set BinPath="D:\1C2GIT\1C2GIT.exe"
set Desctiption="Синхронизация 1С и Git"

sc stop %SrvcName%
sc delete %SrvcName%
sc create %SrvcName% binPath= %BinPath% start= auto displayname= %SrvcName% 

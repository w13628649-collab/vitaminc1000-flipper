/* REA ev_577005b78666fa424ba90d66f60699b2dfcd70f945faeda333b6a91bb4cc0e7e; source response evidence/photon-operation-code-wrapper.json; static pseudocode. */

/* WARNING: Globals starting with '_' overlap smaller symbols at the same address */

void FUN_180998d30(longlong param_1,undefined2 param_2,undefined8 param_3)

{
  code *pcVar1;
  
  if (DAT_18507abad == '\0') {
    FUN_180361910(&DAT_184e2d848);
    DAT_18507abad = '\x01';
  }
  if (*(longlong *)(param_1 + 0x568) != 0) {
    FUN_182ad4100(*(longlong *)(param_1 + 0x568),param_2,param_3,_DAT_184e2d848);
    return;
  }
  FUN_180361b60();
  pcVar1 = (code *)swi(3);
  (*pcVar1)();
  return;
}



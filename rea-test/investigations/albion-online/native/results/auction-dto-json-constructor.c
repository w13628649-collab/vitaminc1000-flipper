/* REA ev_d5eba8610f42f8bad8341201880b2a098503b94a5ef7f1547e6de1c0b641fcc2; source response evidence/auction-dto-json-constructor.json; static pseudocode. */

/* WARNING: Globals starting with '_' overlap smaller symbols at the same address */

void FUN_180b20e70(longlong param_1,undefined8 param_2)

{
  undefined8 uVar1;
  
  if (DAT_18507b86b == '\0') {
    FUN_180361910(&DAT_184dca358);
    DAT_18507b86b = '\x01';
  }
  FUN_1804873c0(param_1,0);
  uVar1 = FUN_181edb0f0(param_2,_DAT_184dca358);
  *(undefined8 *)(param_1 + 0x10) = uVar1;
  FUN_180360c40(param_1 + 0x10,uVar1);
  return;
}



/* REA ev_5297bf2d8ea7ce8c8ed7aa8908d17209596678f26bc978e2a8964d5bceb7ee3b; source response evidence/minimap-interest-area-position-updates.json; static pseudocode. */

/* WARNING: Globals starting with '_' overlap smaller symbols at the same address */

void FUN_18145ae00(longlong param_1)

{
  code *pcVar1;
  char cVar2;
  longlong *plVar3;
  longlong lVar4;
  undefined8 uVar5;
  undefined8 local_70;
  undefined8 uStack_68;
  undefined8 local_60;
  undefined8 uStack_58;
  undefined8 local_50;
  undefined8 local_48;
  undefined8 uStack_40;
  undefined8 local_38;
  undefined8 uStack_30;
  undefined8 local_28;
  
  if (DAT_18508040a == '\0') {
    FUN_180361910(&DAT_184e11d68);
    FUN_180361910(&DAT_184e122d8);
    FUN_180361910(&DAT_184de0b38);
    FUN_180361910(&DAT_184de0c98);
    FUN_180361910(&DAT_184de0df8);
    FUN_180361910(&DAT_184dd0de0);
    FUN_180361910(&DAT_184ddb628);
    FUN_180361910(&DAT_184e0ac30);
    DAT_18508040a = '\x01';
  }
  if (*(longlong *)(param_1 + 0x58) == 0) {
    FUN_180361b60();
    pcVar1 = (code *)swi(3);
    (*pcVar1)();
    return;
  }
  FUN_182a48ea0(&local_48,*(longlong *)(param_1 + 0x58),_DAT_184e11d68);
  local_70 = local_48;
  uStack_68 = uStack_40;
  local_60 = local_38;
  uStack_58 = uStack_30;
  local_50 = local_28;
LAB_18145aee3:
  do {
    do {
      cVar2 = FUN_182cbd010(&local_70,_DAT_184de0c98);
      uVar5 = local_60;
      if (cVar2 == '\0') {
        FUN_1804873c0(&local_70,_DAT_184de0b38);
        return;
      }
      if (*(int *)(_DAT_184e0ac30 + 0xe4) == 0) {
        il2cpp_runtime_class_init();
      }
      if (DAT_18507aae8 == '\0') {
        FUN_180361910(&DAT_184e0ac30);
        DAT_18507aae8 = '\x01';
      }
      if (*(int *)(_DAT_184e0ac30 + 0xe4) == 0) {
        il2cpp_runtime_class_init();
      }
      plVar3 = (longlong *)FUN_180361710(_DAT_184e0ac30);
      if (*plVar3 == 0) goto LAB_18145b052;
      plVar3 = (longlong *)FUN_18096f090(*plVar3,uVar5,0);
    } while (plVar3 == (longlong *)0x0);
  } while ((*(byte *)(*plVar3 + 0x130) < *(byte *)(_DAT_184ddb628 + 0x130)) ||
          (*(longlong *)
            (*(longlong *)(*plVar3 + 200) + -8 + (ulonglong)*(byte *)(_DAT_184ddb628 + 0x130) * 8)
           != _DAT_184ddb628));
  if (*(longlong *)(param_1 + 0x58) != 0) {
    lVar4 = FUN_182b2fa50(*(longlong *)(param_1 + 0x58),uVar5,_DAT_184e122d8);
    uVar5 = (**(code **)(*plVar3 + 0x268))(plVar3,*(undefined8 *)(*plVar3 + 0x270));
    if (lVar4 != 0) {
      FUN_18132f150(lVar4,uVar5);
      goto LAB_18145aee3;
    }
    FUN_180361b60();
  }
  FUN_180361b60();
LAB_18145b052:
  FUN_180361b60();
  FUN_18035da00();
  pcVar1 = (code *)swi(3);
  (*pcVar1)();
  return;
}



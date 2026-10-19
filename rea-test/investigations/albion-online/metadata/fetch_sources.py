import hashlib
import json
import pathlib
import urllib.request

p = pathlib.Path(__file__).parent / 'sources'
j = json.loads((p / 'cpp2il-tree.json').read_text())
wanted = {'Il2CppGlobalMetadataHeader.cs', 'Il2CppMetadata.cs',
          'Il2CppMethodDefinition.cs', 'Il2CppTypeDefinition.cs',
          'Il2CppFieldDefaultValue.cs', 'Il2CppFieldDefinition.cs',
          'Il2CppParameterDefaultValue.cs', 'Il2CppCodeGenModule.cs',
          'Il2CppCodeRegistration.cs', 'Il2CppType.cs',
          'Il2CppAssemblyDefinition.cs', 'Il2CppImageDefinition.cs',
          'Il2CppParameterDefinition.cs', 'Il2CppGlobalMetadataSectionHeader.cs',
          'Il2CppVariableWidthIndex.cs', 'Il2CppMetadataRegistration.cs',
          'Il2CppBinary.cs', 'LibCpp2ILUtils.cs', 'LibCpp2IlUtils.cs',
          'MetadataUsage.cs', 'Il2CppMethodSpec.cs', 'Il2CppGenericInst.cs',
          'Il2CppGenericClass.cs', 'Il2CppGenericContext.cs', 'Il2CppGenericMethodFunctionsDefinitions.cs',
          'Il2CppGenericMethodIndices.cs', 'V29AttributeUtils.cs',
          'Il2CppCustomAttributeDataRange.cs', 'BinaryReaderHelpers.cs',
          'ClassReadingBinaryReader.cs'}
out = []
for x in j['tree']:
    q = x['path']
    if q.rsplit('/', 1)[-1] not in wanted:
        continue
    u = 'https://raw.githubusercontent.com/SamboyCoding/Cpp2IL/' + j['sha'] + '/' + q
    local = p / q.rsplit('/', 1)[-1]
    b = local.read_bytes() if local.exists() else urllib.request.urlopen(u).read()
    local.write_bytes(b)
    out.append(dict(path=q, url=u, sha256=hashlib.sha256(b).hexdigest()))
    print(q)
(p / 'source-manifest.json').write_text(json.dumps(dict(commit=j['sha'], sources=out), indent=2), encoding='utf-8')

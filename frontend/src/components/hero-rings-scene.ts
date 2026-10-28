/** 文件作用说明：构建官网银白、玻璃与钴蓝双环的真实三维场景，供首屏组件控制渲染、尺寸和资源释放。 */
import {
  ACESFilmicToneMapping,
  Color,
  DirectionalLight,
  Group,
  HemisphereLight,
  LatheGeometry,
  Mesh,
  MeshPhysicalMaterial,
  OrthographicCamera,
  PMREMGenerator,
  Scene,
  Shape,
  SRGBColorSpace,
  TorusGeometry,
  WebGLRenderer,
  type BufferGeometry,
  type Material,
  type WebGLRenderTarget,
} from "three";
import { RoomEnvironment } from "three/addons/environments/RoomEnvironment.js";

type HeroRingsScene = {
  render(elapsedSeconds: number): void;
  resize(width: number, height: number, pixelRatio: number): void;
  dispose(): void;
};

/**
 * createBandGeometry 沿闭合圆润截面旋转生成有真实厚度的宽环，避免普通圆管与平面贴图的观感。
 * @param innerRadius - 环内半径。
 * @param outerRadius - 环外半径。
 * @param depth - 环的轴向厚度。
 * @returns 面向相机初始平面的圆润宽环几何体。
 */
function createBandGeometry(innerRadius: number, outerRadius: number, depth: number) {
  const halfDepth = depth / 2;
  const bevel = Math.min((outerRadius - innerRadius) / 3, depth / 4);
  const profile = new Shape();
  profile.moveTo(innerRadius + bevel, -halfDepth);
  profile.lineTo(outerRadius - bevel, -halfDepth);
  profile.quadraticCurveTo(outerRadius, -halfDepth, outerRadius, -halfDepth + bevel);
  profile.lineTo(outerRadius, halfDepth - bevel);
  profile.quadraticCurveTo(outerRadius, halfDepth, outerRadius - bevel, halfDepth);
  profile.lineTo(innerRadius + bevel, halfDepth);
  profile.quadraticCurveTo(innerRadius, halfDepth, innerRadius, halfDepth - bevel);
  profile.lineTo(innerRadius, -halfDepth + bevel);
  profile.quadraticCurveTo(innerRadius, -halfDepth, innerRadius + bevel, -halfDepth);
  const geometry = new LatheGeometry(profile.getPoints(7), 112);
  geometry.rotateX(Math.PI / 2);
  return geometry;
}

/**
 * createHeroRingsScene 创建透明画布上的独立双环，不加载外部模型、图片或环境贴图。
 * @param host - 首屏已预留尺寸的装饰画布容器。
 * @returns 由页面统一驱动的渲染、尺寸同步及幂等释放接口。
 */
export function createHeroRingsScene(host: HTMLElement): HeroRingsScene {
  const renderer = new WebGLRenderer({
    alpha: true,
    antialias: true,
    powerPreference: "low-power",
    premultipliedAlpha: true,
  });
  const scene = new Scene();
  const camera = new OrthographicCamera(-3, 3, 2.5, -2.5, 0.1, 30);
  const geometries = new Set<BufferGeometry>();
  const materials = new Set<Material>();
  let environment: WebGLRenderTarget | null = null;
  let disposed = false;
  const sculpture = new Group();
  const silverRing = new Group();
  const glassRing = new Group();

  /** addMesh 注册模型资源，使初始化失败和正常卸载共用完整清理路径。 */
  function addMesh(group: Group, geometry: BufferGeometry, material: MeshPhysicalMaterial) {
    geometries.add(geometry);
    materials.add(material);
    const mesh = new Mesh(geometry, material);
    group.add(mesh);
    return mesh;
  }

  /** render 让银环绕纵轴、玻璃环绕横轴各自完整慢转，辅以小幅倾转，避免对称轴空转。 */
  function render(elapsedSeconds: number) {
    if (disposed) return;
    const time = Number.isFinite(elapsedSeconds) ? Math.max(0, elapsedSeconds) : 0;
    const silverPhase = ((time % 32) / 32) * Math.PI * 2;
    const glassPhase = ((time % 40) / 40) * Math.PI * 2;
    silverRing.rotation.set(
      0.12 + Math.sin(silverPhase) * 0.18,
      -0.18 + silverPhase,
      -0.26 + Math.sin(silverPhase + 0.4) * 0.08,
    );
    glassRing.rotation.set(
      1.23 - glassPhase,
      0.12 + Math.sin(glassPhase + 1.6) * 0.22,
      -0.18 + Math.sin(glassPhase + 0.5) * 0.12,
    );
    sculpture.position.y = Math.sin(silverPhase * 0.7) * 0.035;
    renderer.render(scene, camera);
  }

  /** resize 同步画布与正交构图，限制像素密度与透射成本，零尺寸时保留上一帧。 */
  function resize(width: number, height: number, pixelRatio: number) {
    if (
      disposed ||
      !Number.isFinite(width) ||
      !Number.isFinite(height) ||
      width <= 0 ||
      height <= 0
    )
      return;
    const aspect = width / height;
    const verticalSpan = Math.max(4.3, 5.1 / aspect);
    camera.left = (-verticalSpan * aspect) / 2;
    camera.right = (verticalSpan * aspect) / 2;
    camera.top = verticalSpan / 2;
    camera.bottom = -verticalSpan / 2;
    camera.updateProjectionMatrix();
    renderer.setPixelRatio(
      Number.isFinite(pixelRatio) ? Math.min(1.5, Math.max(1, pixelRatio)) : 1,
    );
    renderer.setSize(width, height, false);
  }

  /** dispose 幂等释放所有几何、材质、环境与渲染上下文，初始化中途失败也可安全调用。 */
  function dispose() {
    if (disposed) return;
    disposed = true;
    scene.environment = null;
    geometries.forEach((geometry) => geometry.dispose());
    materials.forEach((material) => material.dispose());
    environment?.dispose();
    environment = null;
    scene.clear();
    renderer.dispose();
    renderer.forceContextLoss();
    renderer.domElement.remove();
  }

  try {
    renderer.setClearColor(0xffffff, 0);
    renderer.outputColorSpace = SRGBColorSpace;
    renderer.toneMapping = ACESFilmicToneMapping;
    renderer.toneMappingExposure = 0.98;
    renderer.transmissionResolutionScale = 0.5;
    renderer.domElement.setAttribute("aria-hidden", "true");
    renderer.domElement.style.width = "100%";
    renderer.domElement.style.height = "100%";
    renderer.domElement.style.display = "block";
    camera.position.set(0.15, 0.28, 8);
    camera.lookAt(0, 0, 0);

    const room = new RoomEnvironment();
    let pmrem: PMREMGenerator | null = null;
    try {
      pmrem = new PMREMGenerator(renderer);
      environment = pmrem.fromScene(room, 0.035);
      scene.environment = environment.texture;
    } finally {
      room.dispose();
      pmrem?.dispose();
    }

    const silver = new MeshPhysicalMaterial({
      color: new Color("#d4deed"),
      metalness: 0.98,
      roughness: 0.13,
      envMapIntensity: 1.55,
      clearcoat: 1,
      clearcoatRoughness: 0.1,
    });
    const cobalt = new MeshPhysicalMaterial({
      color: new Color("#1455ff"),
      metalness: 0.68,
      roughness: 0.17,
      envMapIntensity: 1.35,
      clearcoat: 1,
      clearcoatRoughness: 0.08,
    });
    const glass = new MeshPhysicalMaterial({
      color: new Color("#f1f7ff"),
      metalness: 0,
      roughness: 0.065,
      transmission: 0.93,
      thickness: 0.38,
      ior: 1.46,
      attenuationColor: new Color("#c4daff"),
      attenuationDistance: 2.5,
      envMapIntensity: 1.5,
      clearcoat: 1,
      clearcoatRoughness: 0.06,
    });
    // 先登记材质，后续任何几何构建失败也不会遗留尚未挂载的材质。
    [silver, cobalt, glass].forEach((material) => materials.add(material));

    addMesh(silverRing, createBandGeometry(1.01, 1.26, 0.34), silver);
    addMesh(silverRing, createBandGeometry(0.993, 1.019, 0.25), cobalt);
    const silverEdgeGeometry = new TorusGeometry(1.235, 0.018, 8, 112);
    addMesh(silverRing, silverEdgeGeometry, silver).position.z = 0.145;
    addMesh(silverRing, silverEdgeGeometry, silver).position.z = -0.145;
    silverRing.position.set(-0.24, 0.4, 0.05);
    silverRing.scale.set(0.94, 1.14, 1);

    addMesh(glassRing, createBandGeometry(1.1, 1.34, 0.25), glass);
    addMesh(glassRing, createBandGeometry(1.085, 1.17, 0.17), cobalt);
    const glassEdgeGeometry = new TorusGeometry(1.32, 0.016, 8, 112);
    addMesh(glassRing, glassEdgeGeometry, silver).position.z = 0.1;
    addMesh(glassRing, glassEdgeGeometry, silver).position.z = -0.1;
    glassRing.position.set(0.12, -0.48, 0.14);
    glassRing.scale.set(1.24, 1, 1);
    sculpture.add(silverRing, glassRing);
    sculpture.rotation.set(-0.06, -0.12, -0.03);
    scene.add(sculpture);

    scene.add(new HemisphereLight(0xffffff, 0xc9dbf5, 1.2));
    const keyLight = new DirectionalLight(0xffffff, 3.2);
    keyLight.position.set(-3, 5, 5);
    scene.add(keyLight);
    const fillLight = new DirectionalLight(0xcbdcff, 1.4);
    fillLight.position.set(4, 1, 3);
    scene.add(fillLight);
    const rimLight = new DirectionalLight(0xffffff, 2);
    rimLight.position.set(1, -3, -2);
    scene.add(rimLight);
    host.appendChild(renderer.domElement);
    return { render, resize, dispose };
  } catch (error) {
    dispose();
    throw error;
  }
}
